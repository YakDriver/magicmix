package csvio_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/YakDriver/magicmix/internal/csvio"
	"github.com/YakDriver/magicmix/internal/track"
)

func TestLoadWithHeader(t *testing.T) {
	t.Helper()
	data := "Title,Artist,BPM,Energy,Key\n" +
		"Song A,Artist,120,50,1A\n" +
		"Song B,Another,121,60,2B\n"

	path := writeTempFile(t, data)
	tracks, err := csvio.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("Load returned %d tracks, want 2", len(tracks))
	}
	if tracks[0].Title != "Song A" || tracks[0].Key != (track.Key{Number: 1, Mode: track.ModeA}) {
		t.Fatalf("unexpected first track: %+v", tracks[0])
	}
}

func TestLoadWithoutHeader(t *testing.T) {
	t.Helper()
	data := "Song A,Artist,120,50,1A\n" +
		"Song B,Another,121,60,2B\n"

	path := writeTempFile(t, data)
	tracks, err := csvio.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("Load returned %d tracks, want 2", len(tracks))
	}
}

func TestSave(t *testing.T) {
	t.Helper()

	tracks := []track.Track{
		{Title: "Song A", Artist: "Artist", BPM: 120, Energy: 50, Key: track.Key{Number: 1, Mode: track.ModeA}},
		{Title: "Song B", Artist: "Another", BPM: 121.5, Energy: 60, Key: track.Key{Number: 2, Mode: track.ModeB}},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	if err := csvio.Save(context.Background(), path, tracks); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	got := string(content)
	wantHead := "Title,Artist,BPM,Energy,Key\n"
	if len(got) < len(wantHead) || got[:len(wantHead)] != wantHead {
		t.Fatalf("output missing header, got %q", got)
	}
}

func writeTempFile(t *testing.T, data string) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "tracks-*.csv")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := file.WriteString(data); err != nil {
		file.Close()
		t.Fatalf("WriteString: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return file.Name()
}
