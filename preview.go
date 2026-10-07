package main

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	previewCount   = 1  // media files shown in a folder's preview (its cover)
	previewMaxDirs = 20 // folders searched, so huge trees stay fast
)

type previewItem struct {
	Path  string `json:"path"` // relative to the folder being previewed
	MTime int64  `json:"mtime"`
}

type cachedPreview struct {
	mtime time.Time
	items []previewItem
}

// folderPreview returns up to previewCount images or videos for the folder at
// rel. It prefers the folder's own files, then looks breadth-first into
// subfolders. Results are cached until the folder's modification time changes.
func (s *server) folderPreview(rel string) []previewItem {
	st, err := s.root.Stat(filepath.FromSlash(rel))
	if err != nil {
		return nil
	}
	s.previewMu.Lock()
	c, ok := s.previews[rel]
	s.previewMu.Unlock()
	if ok && c.mtime.Equal(st.ModTime()) {
		return c.items
	}

	items := s.findPreview(rel)
	s.previewMu.Lock()
	s.previews[rel] = cachedPreview{mtime: st.ModTime(), items: items}
	s.previewMu.Unlock()
	return items
}

func (s *server) findPreview(rel string) []previewItem {
	var items []previewItem
	queue := []string{"."}
	for visited := 0; len(queue) > 0 && visited < previewMaxDirs; visited++ {
		sub := queue[0]
		queue = queue[1:]
		f, err := s.root.Open(filepath.FromSlash(path.Join(rel, sub)))
		if err != nil {
			continue
		}
		dirents, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			continue
		}
		sort.Slice(dirents, func(i, j int) bool {
			return strings.ToLower(dirents[i].Name()) < strings.ToLower(dirents[j].Name())
		})
		for _, d := range dirents {
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			p := path.Join(sub, name)
			if d.IsDir() {
				queue = append(queue, p)
				continue
			}
			if !d.Type().IsRegular() || kindOf(name) == "other" {
				continue
			}
			info, err := d.Info()
			if err != nil {
				continue
			}
			items = append(items, previewItem{Path: p, MTime: info.ModTime().UnixMilli()})
			if len(items) == previewCount {
				return items
			}
		}
	}
	return items
}
