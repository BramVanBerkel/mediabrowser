package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// writeWAV writes 16-bit mono PCM samples at waveformRate as a WAV file.
func writeWAV(t *testing.T, name string, samples []int16) {
	t.Helper()
	var b bytes.Buffer
	size := uint32(2 * len(samples))
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, 36+size)
	b.WriteString("WAVEfmt ")
	// PCM format: 1 channel, waveformRate Hz, 2 bytes per frame, 16 bits per sample.
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(waveformRate), uint32(2 * waveformRate), uint16(2), uint16(16)} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, size)
	binary.Write(&b, binary.LittleEndian, samples)
	if err := os.WriteFile(name, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWaveform(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("ffmpeg not found; CI installs it so this test runs")
		}
		t.Skip("ffmpeg not found")
	}
	dir := t.TempDir()
	// One second of silence, then one second of a loud 440 Hz tone.
	samples := make([]int16, 2*waveformRate)
	for i := waveformRate; i < len(samples); i++ {
		samples[i] = int16(16000 * math.Sin(2*math.Pi*440*float64(i)/waveformRate))
	}
	writeWAV(t, filepath.Join(dir, "tone.wav"), samples)
	os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("abc"), 0o644)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	h := newServer(root, dir, t.TempDir(), ffmpeg, "").routes()

	for range 2 { // the second request is served from the cache
		rec := get(t, h, "/waveform/tone.wav")
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		var got struct{ Peaks []int }
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		// 2 seconds in 50 ms windows: 20 silent, then 20 at full height.
		if len(got.Peaks) != 40 {
			t.Fatalf("got %d peaks, want 40: %v", len(got.Peaks), got.Peaks)
		}
		for i, p := range got.Peaks {
			if (i < 20 && p > 1) || (i >= 20 && p < 95) {
				t.Fatalf("peaks = %v", got.Peaks)
			}
		}
	}

	for _, url := range []string{"/waveform/a.jpg", "/waveform/missing.wav", "/waveform/"} {
		if rec := get(t, h, url); rec.Code != 404 {
			t.Errorf("%s: status %d, want 404", url, rec.Code)
		}
	}
}

func TestScalePeaks(t *testing.T) {
	got := scalePeaks([]float64{0.1, 0.4, 0.2, 0.2, 0, 0.1}, 3)
	want := []int{100, 50, 25}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := scalePeaks([]float64{0, 0}, 10); !slices.Equal(got, []int{0, 0}) {
		t.Errorf("silence: got %v", got)
	}
}
