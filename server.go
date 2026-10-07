package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true,
	".tif": true, ".tiff": true, ".heic": true, ".heif": true, ".avif": true,
}

var videoExts = map[string]bool{
	".mp4": true, ".m4v": true, ".mov": true, ".webm": true, ".mkv": true, ".avi": true,
	".wmv": true, ".flv": true, ".mpg": true, ".mpeg": true, ".3gp": true, ".mts": true,
	".m2ts": true, ".ts": true, ".ogv": true,
}

var audioExts = map[string]bool{
	".mp3": true, ".m4a": true, ".aac": true, ".wav": true, ".flac": true, ".ogg": true,
	".oga": true, ".opus": true, ".aif": true, ".aiff": true, ".wma": true,
}

func init() {
	// Not every OS ships a mime.types file, so make sure common media types are known.
	for ext, typ := range map[string]string{
		".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm",
		".mkv": "video/x-matroska", ".avi": "video/x-msvideo", ".ogv": "video/ogg",
		".3gp": "video/3gpp", ".ts": "video/mp2t", ".mts": "video/mp2t", ".m2ts": "video/mp2t",
		".heic": "image/heic", ".heif": "image/heif", ".avif": "image/avif", ".webp": "image/webp",
		".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac", ".wav": "audio/wav",
		".flac": "audio/flac", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg",
		".aif": "audio/aiff", ".aiff": "audio/aiff", ".wma": "audio/x-ms-wma",
	} {
		mime.AddExtensionType(ext, typ)
	}
}

func kindOf(name string) string {
	ext := strings.ToLower(path.Ext(name))
	switch {
	case imageExts[ext]:
		return "image"
	case videoExts[ext]:
		return "video"
	case audioExts[ext]:
		return "audio"
	}
	return "other"
}

type server struct {
	root     *os.Root
	rootName string
	auth     *auth
	thumbs   *thumbnailer
	uploadMu sync.Mutex

	previewMu sync.Mutex
	previews  map[string]cachedPreview // folder previews, keyed by folder path
}

func newServer(root *os.Root, rootDir, cacheDir, ffmpeg, password string) *server {
	s := &server{
		root:     root,
		rootName: filepath.Base(rootDir),
		thumbs:   newThumbnailer(root, rootDir, cacheDir, ffmpeg),
		previews: map[string]cachedPreview{},
	}
	if password != "" {
		s.auth = newAuth(password)
	}
	return s
}

func (s *server) routes() http.Handler {
	static, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	index, err := fs.ReadFile(static, "index.html")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /api/list", s.handleList)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.HandleFunc("GET /media/{path...}", s.handleMedia)
	mux.HandleFunc("GET /thumb/{path...}", s.handleThumb)
	mux.HandleFunc("GET /waveform/{path...}", s.handleWaveform)
	mux.HandleFunc("GET /zip/{path...}", s.handleZip)

	if s.auth == nil {
		return mux
	}
	login, err := fs.ReadFile(static, "login.html")
	if err != nil {
		panic(err)
	}
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(login)
	})
	mux.HandleFunc("POST /login", s.auth.handleLogin)
	return s.auth.middleware(mux)
}

// cleanRel turns a user-supplied path into a clean slash-separated path relative
// to the root ("." for the root itself). Hidden path segments are refused.
// Escaping the root is additionally prevented by os.Root.
func cleanRel(p string) (string, bool) {
	c := path.Clean("/" + p)
	if c == "/" {
		return ".", true
	}
	c = c[1:]
	for _, seg := range strings.Split(c, "/") {
		if strings.HasPrefix(seg, ".") {
			return "", false
		}
	}
	return c, true
}

func httpError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, fs.ErrPermission):
		http.Error(w, "permission denied", http.StatusForbidden)
	default:
		http.Error(w, "bad request", http.StatusBadRequest)
	}
}

type entry struct {
	Name    string        `json:"name"`
	Type    string        `json:"type"`
	Size    int64         `json:"size"`
	MTime   int64         `json:"mtime"`
	Preview []previewItem `json:"preview,omitempty"`
}

func (s *server) handleList(w http.ResponseWriter, r *http.Request) {
	rel, ok := cleanRel(r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	f, err := s.root.Open(filepath.FromSlash(rel))
	if err != nil {
		httpError(w, err)
		return
	}
	defer f.Close()
	dirents, err := f.ReadDir(-1)
	if err != nil {
		httpError(w, err)
		return
	}

	entries := []entry{}
	for _, d := range dirents {
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		var info fs.FileInfo
		if d.Type()&fs.ModeSymlink != 0 {
			// Follow symlinks, but only those that stay inside the root.
			info, err = s.root.Stat(filepath.FromSlash(path.Join(rel, name)))
		} else {
			info, err = d.Info()
		}
		if err != nil {
			continue
		}
		e := entry{Name: name, Size: info.Size(), MTime: info.ModTime().UnixMilli()}
		if info.IsDir() {
			e.Type = "dir"
			e.Size = 0
			e.Preview = s.folderPreview(path.Join(rel, name))
		} else if info.Mode().IsRegular() {
			e.Type = kindOf(name)
		} else {
			continue
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if (a.Type == "dir") != (b.Type == "dir") {
			return a.Type == "dir"
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"root":    s.rootName,
		"path":    strings.TrimPrefix(rel, "."),
		"entries": entries,
	})
}

func (s *server) handleMedia(w http.ResponseWriter, r *http.Request) {
	rel, ok := cleanRel(r.PathValue("path"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	f, err := s.root.Open(filepath.FromSlash(rel))
	if err != nil {
		httpError(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.URL.Query().Has("download") {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": st.Name()}))
	}
	// ServeContent handles Range requests, so videos can stream and seek.
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

func (s *server) handleThumb(w http.ResponseWriter, r *http.Request) {
	rel, ok := cleanRel(r.PathValue("path"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	file, err := s.thumbs.get(rel)
	if err != nil {
		w.Header().Set("Cache-Control", "no-cache")
		http.Error(w, "no thumbnail", http.StatusNotFound)
		return
	}
	// The UI adds the file's mtime to the URL, so a changed file gets a new URL.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, file)
}

func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	rel, ok := cleanRel(r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	st, err := s.root.Stat(filepath.FromSlash(rel))
	if err != nil {
		httpError(w, err)
		return
	}
	if !st.IsDir() {
		http.Error(w, "not a folder", http.StatusBadRequest)
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart upload", http.StatusBadRequest)
		return
	}

	saved := []string{}
	var mtime time.Time // original modification time of the next file, if the client sent it
	sub := ""           // subfolder of rel for the next file, if the client sent one
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "upload interrupted", http.StatusBadRequest)
			return
		}
		if part.FileName() == "" && part.FormName() == "lastModified" {
			b, _ := io.ReadAll(io.LimitReader(part, 32))
			part.Close()
			if ms, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && ms > 0 {
				mtime = time.UnixMilli(ms)
			}
			continue
		}
		if part.FileName() == "" && part.FormName() == "dir" {
			b, _ := io.ReadAll(io.LimitReader(part, 4096))
			part.Close()
			sub = sanitizeDir(string(b))
			if sub != "" {
				if err := s.root.MkdirAll(filepath.FromSlash(path.Join(rel, sub)), 0o755); err != nil {
					log.Printf("upload folder %q failed: %v", sub, err)
					http.Error(w, "could not create folder "+sub, http.StatusInternalServerError)
					return
				}
			}
			continue
		}
		name := sanitizeName(part.FileName())
		if name == "" {
			part.Close()
			continue
		}
		dir := path.Join(rel, sub)
		final, err := s.saveUpload(dir, name, part)
		part.Close()
		if err != nil {
			log.Printf("upload %q failed: %v", path.Join(sub, name), err)
			http.Error(w, "could not save "+name, http.StatusInternalServerError)
			return
		}
		if !mtime.IsZero() && mtime.Before(time.Now().Add(24*time.Hour)) {
			s.root.Chtimes(filepath.FromSlash(path.Join(dir, final)), mtime, mtime)
		}
		mtime = time.Time{}
		log.Printf("uploaded %s (from %s)", path.Join(dir, final), r.RemoteAddr)
		saved = append(saved, path.Join(sub, final))
		sub = ""
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"saved": saved})
}

// sanitizeName reduces a client-supplied filename to a safe, visible base name.
func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(strings.TrimLeft(name, "."))
	return name
}

// sanitizeDir reduces a client-supplied relative folder path, such as
// "Trip/Day 1" from an uploaded folder, to safe, visible names joined by "/".
// It returns "" if nothing is left.
func sanitizeDir(dir string) string {
	var parts []string
	for _, p := range strings.Split(strings.ReplaceAll(dir, "\\", "/"), "/") {
		if p = sanitizeName(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}

// saveUpload streams src into dir under name, picking "name (1).ext" etc. if
// the name is taken. It writes to a hidden temp file first so partial uploads
// never show up in the listing.
func (s *server) saveUpload(dir, name string, src io.Reader) (string, error) {
	tmp, err := createTemp(s.root, dir)
	if err != nil {
		return "", err
	}
	tmpName := path.Join(dir, tmp.Name())
	_, err = io.Copy(tmp, src)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		s.root.Remove(filepath.FromSlash(tmpName))
		return "", err
	}

	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 10000; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		target := filepath.FromSlash(path.Join(dir, candidate))
		if _, err := s.root.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := s.root.Rename(filepath.FromSlash(tmpName), target); err != nil {
			s.root.Remove(filepath.FromSlash(tmpName))
			return "", err
		}
		return candidate, nil
	}
	s.root.Remove(filepath.FromSlash(tmpName))
	return "", errors.New("too many files with the same name")
}

// createTemp makes a new hidden file in dir. The returned file's Name() is its base name.
func createTemp(root *os.Root, dir string) (*namedFile, error) {
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf(".upload-%s.part", randomHex(8))
		f, err := root.OpenFile(filepath.FromSlash(path.Join(dir, name)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return &namedFile{File: f, name: name}, nil
	}
	return nil, errors.New("could not create temp file")
}

type namedFile struct {
	*os.File
	name string
}

func (f *namedFile) Name() string { return f.name }
