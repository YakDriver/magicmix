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

func TestLoadExtendedColumns(t *testing.T) {
	// Mirrors the 4bbqpaa.csv layout: extra/unknown columns, punctuation in
	// headers ("POP."), and a column order different from the legacy layout.
	data := "#,TITLE,ARTIST,RELEASE,ADDED,BPM,ENERGY,DANCE,LOUD,VALENCE,LENGTH,ACOUSTIC,POP.,A.SEP,RND,Key\n" +
		"174,hate that i made you love me,Ariana Grande,2026-05-29,2026-07-02,96,45,63,-8,45,3:17,69,100,150,9894,3A\n"

	path := writeTempFile(t, data)
	tracks, err := csvio.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("Load returned %d tracks, want 1", len(tracks))
	}

	got := tracks[0]
	if got.Title != "hate that i made you love me" || got.Artist != "Ariana Grande" {
		t.Fatalf("unexpected identity: %+v", got)
	}
	if got.BPM != 96 || got.Energy != 45 || got.Key != (track.Key{Number: 3, Mode: track.ModeA}) {
		t.Fatalf("unexpected core signals: %+v", got)
	}
	assertSignal(t, "danceability", got.Danceability, 63)
	assertSignal(t, "valence", got.Valence, 45)
	assertSignal(t, "acousticness", got.Acousticness, 69)
	assertSignal(t, "popularity", got.Popularity, 100)
}

func TestLoadOptionalSignalsAbsent(t *testing.T) {
	// Only the core columns are present; extended signals must be nil.
	data := "Title,Artist,BPM,Energy,Key\n" +
		"Song A,Artist,120,50,1A\n"

	path := writeTempFile(t, data)
	tracks, err := csvio.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	got := tracks[0]
	if got.Danceability != nil || got.Valence != nil || got.Popularity != nil || got.Acousticness != nil {
		t.Fatalf("expected nil optional signals, got %+v", got)
	}
}

func TestLoadMissingCoreColumnFallsBackToPositional(t *testing.T) {
	// No recognizable header (energy/key missing by name) and the first row is
	// not valid positional data, so parsing should fail clearly rather than
	// silently mangling data.
	data := "Title,Artist,Tempo\n" +
		"Song A,Artist,120\n"

	path := writeTempFile(t, data)
	if _, err := csvio.Load(context.Background(), path); err == nil {
		t.Fatal("expected error for file lacking core columns, got nil")
	}
}

func assertSignal(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: expected %d, got nil", name, want)
	}
	if *got != want {
		t.Fatalf("%s: expected %d, got %d", name, want, *got)
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

func TestSaveRoundTripsExtendedSignals(t *testing.T) {
	d, v := 63, 45
	tracks := []track.Track{
		{Title: "A", Artist: "X", BPM: 120, Energy: 50, Key: track.Key{Number: 1, Mode: track.ModeA}, Danceability: &d, Valence: &v},
		{Title: "B", Artist: "Y", BPM: 121, Energy: 60, Key: track.Key{Number: 2, Mode: track.ModeB}},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.csv")
	if err := csvio.Save(context.Background(), path, tracks); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	reloaded, err := csvio.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(reloaded) != 2 {
		t.Fatalf("got %d tracks, want 2", len(reloaded))
	}
	if reloaded[0].Danceability == nil || *reloaded[0].Danceability != 63 {
		t.Fatalf("danceability not preserved: %+v", reloaded[0])
	}
	if reloaded[0].Valence == nil || *reloaded[0].Valence != 45 {
		t.Fatalf("valence not preserved: %+v", reloaded[0])
	}
	// Second track had no danceability; it must round-trip as absent.
	if reloaded[1].Danceability != nil {
		t.Fatalf("expected absent danceability to stay nil, got %v", *reloaded[1].Danceability)
	}
}

func TestSaveLegacyStaysFiveColumns(t *testing.T) {
	tracks := []track.Track{
		{Title: "A", Artist: "X", BPM: 120, Energy: 50, Key: track.Key{Number: 1, Mode: track.ModeA}},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.csv")
	if err := csvio.Save(context.Background(), path, tracks); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	wantHead := "Title,Artist,BPM,Energy,Key\n"
	if got := string(content); len(got) < len(wantHead) || got[:len(wantHead)] != wantHead {
		t.Fatalf("legacy output should keep 5-column header, got %q", got)
	}
}

func writeTempFile(t *testing.T, data string) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "tracks-*.csv")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := file.WriteString(data); err != nil {
		_ = file.Close()
		t.Fatalf("WriteString: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return file.Name()
}
