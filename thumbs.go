package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
	"golang.org/x/sync/singleflight"
)

// thumbSize is the length of a thumbnail's short side in pixels. The grid
// crops thumbnails to squares, so the short side is what matters.
const thumbSize = 320

var errNoThumb = errors.New("no thumbnail available")

// Image formats the Go decoders registered above can read.
var goImageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".bmp": true, ".tif": true, ".tiff": true,
}

type thumbnailer struct {
	root     *os.Root
	rootDir  string
	cacheDir string
	ffmpeg   string
	sem      chan struct{} // limits concurrent generation (decoding is memory hungry)
	group    singleflight.Group

	mu     sync.Mutex
	failed map[string]bool // cache files that could not be made this run
}

func newThumbnailer(root *os.Root, rootDir, cacheDir, ffmpeg string) *thumbnailer {
	return &thumbnailer{
		root:     root,
		rootDir:  rootDir,
		cacheDir: cacheDir,
		ffmpeg:   ffmpeg,
		sem:      make(chan struct{}, min(runtime.NumCPU(), 4)),
		failed:   map[string]bool{},
	}
}

// get returns the path of a cached JPEG thumbnail for rel, generating it if needed.
func (t *thumbnailer) get(rel string) (string, error) {
	kind := kindOf(rel)
	if kind == "other" {
		return "", errNoThumb
	}
	return t.cached(rel, ".jpg", func(out string) error { return t.generate(rel, kind, out) })
}

// cached returns the path of the cache file with extension ext that gen makes
// from rel, calling gen only if there is none yet. Cache files are keyed by
// rel's path, size and modification time, so a changed file gets new ones.
func (t *thumbnailer) cached(rel, ext string, gen func(out string) error) (string, error) {
	st, err := t.root.Stat(filepath.FromSlash(rel))
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errNoThumb
	}
	sum := sha1.Sum(fmt.Appendf(nil, "%s\x00%d\x00%d", rel, st.Size(), st.ModTime().UnixNano()))
	key := hex.EncodeToString(sum[:])
	out := filepath.Join(t.cacheDir, key[:2], key+ext)
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}

	t.mu.Lock()
	failed := t.failed[key+ext]
	t.mu.Unlock()
	if failed {
		return "", errNoThumb
	}

	_, err, _ = t.group.Do(key+ext, func() (any, error) {
		if _, err := os.Stat(out); err == nil {
			return nil, nil
		}
		t.sem <- struct{}{}
		defer func() { <-t.sem }()
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		err := gen(out)
		if err != nil {
			t.mu.Lock()
			t.failed[key+ext] = true
			t.mu.Unlock()
		}
		return nil, err
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

func (t *thumbnailer) generate(rel, kind, out string) error {
	if kind == "image" && goImageExts[strings.ToLower(path.Ext(rel))] {
		err := t.fromImage(rel, out)
		if err == nil || t.ffmpeg == "" {
			return err
		}
		// Let ffmpeg have a go at images Go couldn't decode.
	}
	if t.ffmpeg == "" {
		return errNoThumb
	}
	return t.fromFFmpeg(rel, kind, out)
}

func (t *thumbnailer) fromImage(rel, out string) error {
	f, err := t.root.Open(filepath.FromSlash(rel))
	if err != nil {
		return err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	orientation := 1
	if _, err := f.Seek(0, io.SeekStart); err == nil {
		if x, err := exif.Decode(f); err == nil {
			if tag, err := x.Get(exif.Orientation); err == nil {
				if v, err := tag.Int(0); err == nil {
					orientation = v
				}
			}
		}
	}
	return writeJPEG(orient(resize(src), orientation), out)
}

func (t *thumbnailer) fromFFmpeg(rel, kind, out string) error {
	in := filepath.Join(t.rootDir, filepath.FromSlash(rel))
	tmp := out + ".ffmpeg.jpg"
	defer os.Remove(tmp)

	if kind == "image" {
		// Still images (e.g. HEIC) are often tiled, which ffmpeg won't combine
		// with a scale filter, so decode at full size and resize in Go.
		if err := t.runFFmpeg("-i", in, "-frames:v", "1", "-q:v", "2", tmp); err != nil {
			return err
		}
		f, err := os.Open(tmp)
		if err != nil {
			return err
		}
		defer f.Close()
		src, err := jpeg.Decode(f)
		if err != nil {
			return err
		}
		return writeJPEG(resize(src), out)
	}

	scale := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase", thumbSize, thumbSize)
	if kind == "audio" {
		// Audio files can carry cover art, which ffmpeg sees as a one-frame video stream.
		if err := t.runFFmpeg("-i", in, "-map", "0:v:0", "-frames:v", "1", "-vf", scale, "-q:v", "4", tmp); err != nil {
			return err
		}
		return os.Rename(tmp, out)
	}

	// Grab a frame one second in to skip black intros; retry at the start for very short clips.
	for _, ss := range []string{"1", "0"} {
		if err := t.runFFmpeg("-ss", ss, "-i", in, "-frames:v", "1", "-vf", scale, "-q:v", "4", tmp); err == nil {
			return os.Rename(tmp, out)
		}
	}
	return errNoThumb
}

// runFFmpeg runs ffmpeg with args, whose last element is the output file, and
// succeeds only if that file was written.
func (t *thumbnailer) runFFmpeg(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args = append([]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}, args...)
	if err := exec.CommandContext(ctx, t.ffmpeg, args...).Run(); err != nil {
		return err
	}
	if st, err := os.Stat(args[len(args)-1]); err != nil || st.Size() == 0 {
		return errNoThumb
	}
	return nil
}

// resize scales src so its short side is thumbSize (never upscaling), on a
// white background so transparent images don't turn black in the JPEG.
func resize(src image.Image) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := min(1, float64(thumbSize)/float64(min(w, h)))
	nw := max(1, int(float64(w)*scale+0.5))
	nh := max(1, int(float64(h)*scale+0.5))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

// orient applies an EXIF orientation (1-8) so the image displays upright.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := sw, sh
	if o >= 5 {
		dw, dh = sh, sw
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch o {
			case 2: // mirror horizontal
				sx, sy = sw-1-x, y
			case 3: // rotate 180
				sx, sy = sw-1-x, sh-1-y
			case 4: // mirror vertical
				sx, sy = x, sh-1-y
			case 5: // transpose
				sx, sy = y, x
			case 6: // rotate 90 clockwise
				sx, sy = y, sh-1-x
			case 7: // transverse
				sx, sy = sw-1-y, sh-1-x
			case 8: // rotate 90 counter-clockwise
				sx, sy = sw-1-y, x
			}
			si := src.PixOffset(sx, sy)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}

func writeJPEG(img image.Image, out string) error {
	f, err := os.CreateTemp(filepath.Dir(out), "tmp-*.jpg")
	if err != nil {
		return err
	}
	err = jpeg.Encode(f, img, &jpeg.Options{Quality: 80})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), out)
}
