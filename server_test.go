package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// newTestServer serves a small tree:
//
//	a.jpg (3 bytes), .hidden, sub/b.txt (5 bytes), sub/deeper/, .secret/c.txt
func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"a.jpg":         "abc",
		".hidden":       "xx",
		"sub/b.txt":     "hello",
		".secret/c.txt": "nope",
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return newServer(root, dir, t.TempDir(), "", "").routes()
}

func get(t *testing.T, h http.Handler, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
	return rec
}

func TestStats(t *testing.T) {
	h := newTestServer(t)
	rec := get(t, h, "/api/stats?path=")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct {
		Size, Files, Folders int64
		Partial              bool
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// Hidden files and folders are not counted.
	if got.Size != 8 || got.Files != 2 || got.Folders != 2 || got.Partial {
		t.Errorf("got %+v, want size 8, 2 files, 2 folders", got)
	}

	rec = get(t, h, "/api/stats?path=sub")
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Size != 5 || got.Files != 1 || got.Folders != 1 {
		t.Errorf("sub: got %+v, want size 5, 1 file, 1 folder", got)
	}
}

func TestZip(t *testing.T) {
	h := newTestServer(t)
	rec := get(t, h, "/zip/sub")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename=sub.zip` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Name == "sub/b.txt" {
			rc, _ := f.Open()
			body, _ := io.ReadAll(rc)
			rc.Close()
			if string(body) != "hello" {
				t.Errorf("b.txt = %q", body)
			}
		}
	}
	sort.Strings(names)
	want := []string{"sub/b.txt", "sub/deeper/"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Errorf("entries = %v, want %v", names, want)
	}

	// The whole root leaves out hidden entries.
	rec = get(t, h, "/zip/")
	zr, err = zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if bytes.Contains([]byte(f.Name), []byte("/.")) {
			t.Errorf("hidden entry %q in zip", f.Name)
		}
	}
}

func TestRefusedPaths(t *testing.T) {
	h := newTestServer(t)
	for _, url := range []string{
		"/api/stats?path=.secret",
		"/api/stats?path=missing",
		"/zip/.secret",
		"/zip/a.jpg",
	} {
		if rec := get(t, h, url); rec.Code == 200 {
			t.Errorf("%s: got 200", url)
		}
	}
}
