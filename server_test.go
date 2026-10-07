package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
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

// upload posts files to /api/upload?path=dest; each one goes into the
// subfolder given by its key, as an uploaded folder's files do.
func upload(t *testing.T, h http.Handler, dest string, files [][2]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, f := range files {
		dir, name := f[0], f[1]
		if dir != "" {
			mw.WriteField("dir", dir)
		}
		fw, _ := mw.CreateFormFile("file", name)
		fw.Write([]byte("data:" + name))
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/api/upload?path="+dest, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUploadFolder(t *testing.T) {
	h := newTestServer(t)
	rec := upload(t, h, "sub", [][2]string{
		{"Trip/Day 1", "x.jpg"},
		{"../Trip/./Day 1", "y.jpg"}, // cleaned up to the same folder
		{"", "z.jpg"},                // no folder: straight into sub
		{"Trip/Day 1", "x.jpg"},      // taken, so renamed
	})
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got struct{ Saved []string }
	json.Unmarshal(rec.Body.Bytes(), &got)
	want := []string{"Trip/Day 1/x.jpg", "Trip/Day 1/y.jpg", "z.jpg", "Trip/Day 1/x (1).jpg"}
	if len(got.Saved) != len(want) {
		t.Fatalf("saved %q, want %q", got.Saved, want)
	}
	for i := range want {
		if got.Saved[i] != want[i] {
			t.Errorf("saved %q, want %q", got.Saved, want)
			break
		}
	}
	for _, p := range []string{"sub/Trip/Day%201/x.jpg", "sub/Trip/Day%201/y.jpg", "sub/z.jpg"} {
		if rec := get(t, h, "/media/"+p); rec.Code != 200 || !bytes.HasPrefix(rec.Body.Bytes(), []byte("data:")) {
			t.Errorf("%s: status %d", p, rec.Code)
		}
	}

	// A folder can't replace a file of the same name.
	if rec := upload(t, h, "", [][2]string{{"a.jpg", "w.jpg"}}); rec.Code == 200 {
		t.Errorf("folder over a file: got 200")
	}
}

func TestAudio(t *testing.T) {
	h := newTestServer(t)
	upload(t, h, "", [][2]string{{"album", "01 song.mp3"}, {"album", "cover.jpg"}})

	var got struct{ Entries []entry }
	json.Unmarshal(get(t, h, "/api/list?path=album").Body.Bytes(), &got)
	types := map[string]string{}
	for _, e := range got.Entries {
		types[e.Name] = e.Type
	}
	if types["01 song.mp3"] != "audio" || types["cover.jpg"] != "image" {
		t.Errorf("types = %v", types)
	}

	// Audio is left out of folder previews, which need a picture.
	json.Unmarshal(get(t, h, "/api/list?path=").Body.Bytes(), &got)
	for _, e := range got.Entries {
		if e.Name == "album" && (len(e.Preview) != 1 || e.Preview[0].Path != "cover.jpg") {
			t.Errorf("album preview = %+v, want just cover.jpg", e.Preview)
		}
	}

	if ct := get(t, h, "/media/album/01%20song.mp3").Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestMediaSandbox(t *testing.T) {
	h := newTestServer(t)
	upload(t, h, "", [][2]string{{"", "page.html"}, {"", "logo.svg"}, {"", "doc.pdf"}, {"", "song.mp3"}, {"", "data.bin"}})

	for name, sandboxed := range map[string]bool{
		"page.html": true, "logo.svg": true, "data.bin": true, "sub/b.txt": true,
		"a.jpg": false, "doc.pdf": false, "song.mp3": false,
	} {
		rec := get(t, h, "/media/"+name)
		if rec.Code != 200 {
			t.Fatalf("%s: status %d", name, rec.Code)
		}
		if got := rec.Header().Get("Content-Security-Policy") == "sandbox"; got != sandboxed {
			t.Errorf("%s: sandboxed = %v, want %v", name, got, sandboxed)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: no nosniff header", name)
		}
	}
	if get(t, h, "/api/list?path=").Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("/api/list: no nosniff header")
	}
}
