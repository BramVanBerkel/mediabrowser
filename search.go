package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"
	"strings"
)

const searchLimit = 500

type searchResult struct {
	entry
	Path string `json:"path"` // relative to the searched folder
}

// handleSearch finds files and folders below ?path= whose names contain every
// word of ?q=, ignoring case.
func (s *server) handleSearch(w http.ResponseWriter, r *http.Request) {
	rel, ok := cleanRel(r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	words := strings.Fields(strings.ToLower(r.URL.Query().Get("q")))

	results := []searchResult{}
	truncated := false
	err := fs.WalkDir(s.root.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == rel {
				return err
			}
			return nil // skip unreadable folders
		}
		if p == rel || len(words) == 0 {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		lower := strings.ToLower(name)
		for _, w := range words {
			if !strings.Contains(lower, w) {
				return nil
			}
		}
		if len(results) == searchLimit {
			truncated = true
			return fs.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		sub := p
		if rel != "." {
			sub = strings.TrimPrefix(p, rel+"/")
		}
		e := entry{Name: name, Size: info.Size(), MTime: info.ModTime().UnixMilli()}
		switch {
		case d.IsDir():
			e.Type = "dir"
			e.Size = 0
			e.Preview = s.folderPreview(p)
		case d.Type().IsRegular():
			e.Type = kindOf(name)
		default:
			return nil
		}
		results = append(results, searchResult{entry: e, Path: sub})
		return nil
	})
	if err != nil {
		httpError(w, err)
		return
	}

	sort.Slice(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if (a.Type == "dir") != (b.Type == "dir") {
			return a.Type == "dir"
		}
		return strings.ToLower(a.Path) < strings.ToLower(b.Path)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"root":      s.rootName,
		"path":      strings.TrimPrefix(rel, "."),
		"results":   results,
		"truncated": truncated,
	})
}
