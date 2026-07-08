package csvio_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YakDriver/magicmix/internal/csvio"
)

// A messy real-world header: leading "#" index column, reordered/extra columns
// (ADDED, LOUD, A.SEP, RND), CRLF line endings, and a quoted field with a comma.
const passthroughInput = "#,TITLE,ARTIST,Key,RELEASE,ADDED,BPM,ENERGY,DANCE,LOUD,VALENCE,LENGTH,ACOUSTIC,POP.,A.SEP,RND\r\n" +
	"1,American Pie,Don McLean,9B,1971,2026-07-03,138,48,53,-12,49,8:36,70,84,4,2064\r\n" +
	"2,\"I Knew It, I Knew You\",Taylor Swift,8B,2026,2026-07-03,93,52,70,-8,60,2:58,22,94,5,1151\r\n" +
	"3,Mr. Brightside,The Killers,3B,2004,2026-07-03,148,91,35,-5,24,3:42,0,95,6,1018\r\n"

func TestSaveInFormatPassthrough(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.csv")
	if err := os.WriteFile(in, []byte(passthroughInput), 0o644); err != nil {
		t.Fatal(err)
	}

	pl, err := csvio.LoadPlaylist(context.Background(), in)
	if err != nil {
		t.Fatalf("LoadPlaylist: %v", err)
	}
	if len(pl.Tracks) != 3 {
		t.Fatalf("got %d tracks, want 3", len(pl.Tracks))
	}
	if !pl.CRLF {
		t.Error("expected CRLF to be detected")
	}

	// Reorder: swap first and last tracks.
	pl.Tracks[0], pl.Tracks[2] = pl.Tracks[2], pl.Tracks[0]

	out := filepath.Join(dir, "out.csv")
	if err := csvio.SaveInFormat(context.Background(), out, pl); err != nil {
		t.Fatalf("SaveInFormat: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	// Header preserved verbatim, CRLF preserved.
	if !strings.HasPrefix(got, "#,TITLE,ARTIST,Key,RELEASE,ADDED,BPM,ENERGY,DANCE,LOUD,VALENCE,LENGTH,ACOUSTIC,POP.,A.SEP,RND\r\n") {
		t.Fatalf("header not preserved verbatim:\n%q", got)
	}
	if !strings.Contains(got, "\r\n") {
		t.Error("CRLF line endings not preserved")
	}

	lines := strings.Split(strings.TrimRight(got, "\r\n"), "\r\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 (header + 3 rows)", len(lines))
	}
	// Rows reordered: Mr. Brightside now first, American Pie last.
	if !strings.HasPrefix(lines[1], "3,Mr. Brightside,") {
		t.Errorf("first data row = %q, want Mr. Brightside", lines[1])
	}
	if !strings.HasPrefix(lines[3], "1,American Pie,") {
		t.Errorf("last data row = %q, want American Pie", lines[3])
	}
	// Extraneous columns (the tail) preserved verbatim on a row.
	if !strings.HasSuffix(lines[3], ",8:36,70,84,4,2064") {
		t.Errorf("extra columns not preserved on American Pie row: %q", lines[3])
	}
	// The quoted comma field survives the round trip.
	if !strings.Contains(got, "\"I Knew It, I Knew You\"") {
		t.Errorf("quoted comma field not preserved:\n%s", got)
	}
}

func TestSaveInFormatFallsBackToCanonicalWithoutRaw(t *testing.T) {
	// A Playlist whose tracks have no Raw (e.g. synthesized) falls back to canonical.
	pl, err := csvio.LoadPlaylist(context.Background(), writeCanonicalFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	// Strip Raw to simulate synthesized tracks.
	for i := range pl.Tracks {
		pl.Tracks[i].Raw = nil
	}
	pl.Header = nil

	out := filepath.Join(t.TempDir(), "out.csv")
	if err := csvio.SaveInFormat(context.Background(), out, pl); err != nil {
		t.Fatalf("SaveInFormat: %v", err)
	}
	data, _ := os.ReadFile(out)
	if !strings.HasPrefix(string(data), "Title,Artist,BPM,Energy,Key") {
		t.Fatalf("expected canonical header, got:\n%s", string(data))
	}
}

func writeCanonicalFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "canon.csv")
	content := "Title,Artist,BPM,Energy,Key\nA,X,120,50,8A\nB,Y,122,60,9A\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
