package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

const (
	waveformRate   = 8000              // samples per second ffmpeg decodes to; plenty to measure loudness
	waveformWindow = waveformRate / 20 // samples per measured window (50 ms)
	waveformBars   = 1000              // most values sent; the viewer combines them to fit its width
)

// waveform returns the path of a cached JSON file with the loudness of the
// audio file at rel over its length, made with ffmpeg if needed.
func (t *thumbnailer) waveform(rel string) (string, error) {
	if kindOf(rel) != "audio" || t.ffmpeg == "" {
		return "", errNoThumb
	}
	return t.cached(rel, ".wave.json", func(out string) error { return t.makeWaveform(rel, out) })
}

func (t *thumbnailer) makeWaveform(rel, out string) error {
	in := filepath.Join(t.rootDir, filepath.FromSlash(rel))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// Decode the first audio stream to mono 16-bit samples on stdout, so even
	// long files never sit in memory as a whole.
	cmd := exec.CommandContext(ctx, t.ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin",
		"-i", in, "-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(waveformRate), "-f", "s16le", "-")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	levels, err := windowLevels(stdout)
	if werr := cmd.Wait(); err == nil {
		err = werr
	}
	if err != nil {
		return err
	}
	if len(levels) == 0 {
		return errNoThumb
	}

	data, err := json.Marshal(map[string]any{"peaks": scalePeaks(levels, waveformBars)})
	if err != nil {
		return err
	}
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}

// windowLevels reads 16-bit little-endian mono samples and returns the RMS
// loudness (0 to 1) of every waveformWindow of them.
func windowLevels(r io.Reader) ([]float64, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	var levels []float64
	var sum float64
	n := 0
	var buf [2]byte
	for {
		if _, err := io.ReadFull(br, buf[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, err
		}
		v := float64(int16(binary.LittleEndian.Uint16(buf[:]))) / 32768
		sum += v * v
		if n++; n == waveformWindow {
			levels = append(levels, math.Sqrt(sum/float64(n)))
			sum, n = 0, 0
		}
	}
	if n > 0 {
		levels = append(levels, math.Sqrt(sum/float64(n)))
	}
	return levels, nil
}

// scalePeaks reduces levels to at most n values, each the loudest window it
// covers, scaled so the loudest of all is 100.
func scalePeaks(levels []float64, n int) []int {
	n = min(n, len(levels))
	peaks := make([]float64, n)
	for i, v := range levels {
		b := i * n / len(levels)
		peaks[b] = max(peaks[b], v)
	}
	top := slices.Max(peaks)
	out := make([]int, n)
	for i, v := range peaks {
		if top > 0 {
			out[i] = int(math.Round(v / top * 100))
		}
	}
	return out
}

func (s *server) handleWaveform(w http.ResponseWriter, r *http.Request) {
	rel, ok := cleanRel(r.PathValue("path"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	file, err := s.thumbs.waveform(rel)
	if err != nil {
		w.Header().Set("Cache-Control", "no-cache")
		http.Error(w, "no waveform", http.StatusNotFound)
		return
	}
	// Like thumbnails, the UI adds the file's mtime to the URL.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "application/json")
	http.ServeFile(w, r, file)
}
