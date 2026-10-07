package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	statsMaxEntries = 200_000         // entries counted before giving up on a huge tree
	statsMaxTime    = 5 * time.Second // time spent counting before giving up
)

// walkVisible calls fn for every file and folder below rel, leaving out rel
// itself, hidden entries and unreadable folders. Like search, it does not
// follow symlinks, so loops are impossible. sub is the path relative to rel.
func (s *server) walkVisible(rel string, fn func(p, sub string, d fs.DirEntry) error) error {
	return fs.WalkDir(s.root.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == rel {
				return err
			}
			return nil // skip unreadable folders
		}
		if p == rel {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		sub := p
		if rel != "." {
			sub = strings.TrimPrefix(p, rel+"/")
		}
		return fn(p, sub, d)
	})
}

// statDir resolves ?path= or {path...} to a folder below the root.
func (s *server) statDir(w http.ResponseWriter, p string) (string, fs.FileInfo, bool) {
	rel, ok := cleanRel(p)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return "", nil, false
	}
	st, err := s.root.Stat(filepath.FromSlash(rel))
	if err != nil {
		httpError(w, err)
		return "", nil, false
	}
	if !st.IsDir() {
		http.Error(w, "not a folder", http.StatusBadRequest)
		return "", nil, false
	}
	return rel, st, true
}

// handleStats adds up the size and number of files and folders below ?path=,
// for the details panel. Very large trees are cut short and marked partial.
func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	rel, st, ok := s.statDir(w, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	var size, files, folders, seen int64
	partial := false
	deadline := time.Now().Add(statsMaxTime)
	err := s.walkVisible(rel, func(p, sub string, d fs.DirEntry) error {
		seen++
		if seen > statsMaxEntries || seen%1024 == 0 && (r.Context().Err() != nil || time.Now().After(deadline)) {
			partial = true
			return fs.SkipAll
		}
		switch {
		case d.IsDir():
			folders++
		case d.Type().IsRegular():
			files++
			if info, err := d.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	if err != nil {
		httpError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"size":    size,
		"files":   files,
		"folders": folders,
		"mtime":   st.ModTime().UnixMilli(),
		"partial": partial,
	})
}

// handleZip streams the folder at {path...} as a zip archive, with everything
// inside a top-level folder of the same name.
func (s *server) handleZip(w http.ResponseWriter, r *http.Request) {
	rel, _, ok := s.statDir(w, r.PathValue("path"))
	if !ok {
		return
	}
	name := s.rootName
	if rel != "." {
		name = path.Base(rel)
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".zip"}))

	zw := zip.NewWriter(w)
	err := s.walkVisible(rel, func(p, sub string, d fs.DirEntry) error {
		if err := r.Context().Err(); err != nil {
			return err // the client went away
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		switch {
		case d.IsDir():
			_, err = zw.CreateHeader(&zip.FileHeader{Name: name + "/" + sub + "/", Modified: info.ModTime()})
			return err
		case d.Type().IsRegular():
			return s.zipFile(zw, p, name+"/"+sub, info)
		}
		return nil
	})
	if err == nil {
		err = zw.Close()
	}
	if err != nil {
		log.Printf("zip %s failed: %v", rel, err)
		return
	}
	log.Printf("zipped %s (for %s)", rel, r.RemoteAddr)
}

func (s *server) zipFile(zw *zip.Writer, p, name string, info fs.FileInfo) error {
	f, err := s.root.Open(filepath.FromSlash(p))
	if err != nil {
		return nil // skip unreadable files
	}
	defer f.Close()
	hdr := &zip.FileHeader{Name: name, Modified: info.ModTime(), Method: zip.Deflate}
	if kindOf(name) != "other" {
		hdr.Method = zip.Store // photos and videos are already compressed
	}
	dst, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, f)
	return err
}
