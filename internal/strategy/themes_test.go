package strategy

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/YakDriver/magicmix/internal/track"
)

// themedTracks builds a varied set (valence, energy, ~3-min durations) so multiple
// pots activate and time-based chapter sizing applies.
func themedTracks(n int) []track.Track {
	keys := []string{"1A", "2A", "3A", "4A", "5A", "8B", "9B", "10B", "11B", "12B"}
	tracks := make([]track.Track, n)
	for i := range tracks {
		v := (i * 37) % 100
		dur := 170 + (i*7)%40 // ~3 min each
		tracks[i] = track.Track{
			Title:    fmt.Sprintf("t%02d", i),
			BPM:      115 + float64(i%15),
			Energy:   20 + (i*13)%80,
			Key:      mustKey(keys[i%len(keys)]),
			Valence:  &v,
			Duration: &dur,
		}
	}
	return tracks
}

func mustKey(s string) track.Key {
	k, err := track.ParseKey(s)
	if err != nil {
		panic(err)
	}
	return k
}

func titlesOf(tracks []track.Track) string {
	s := ""
	for _, t := range tracks {
		s += t.Title + "|"
	}
	return s
}

func TestThemesPlacesEveryTrack(t *testing.T) {
	tracks := themedTracks(60)
	ordered, err := NewThemesSorter().Sort(WithSeed(context.Background(), 7), tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	if len(ordered) != len(tracks) {
		t.Fatalf("themes placed %d tracks, want %d (it must not drop any)", len(ordered), len(tracks))
	}
	seen := make(map[string]int)
	for _, tr := range ordered {
		seen[tr.Title]++
	}
	for _, tr := range tracks {
		if seen[tr.Title] != 1 {
			t.Fatalf("track %q appears %d times, want exactly 1", tr.Title, seen[tr.Title])
		}
	}
}

func TestThemesDeterministicPerSeed(t *testing.T) {
	tracks := themedTracks(60)
	first, _ := NewThemesSorter().Sort(WithSeed(context.Background(), 99), tracks)
	second, _ := NewThemesSorter().Sort(WithSeed(context.Background(), 99), tracks)
	if titlesOf(first) != titlesOf(second) {
		t.Fatal("themes must be deterministic for a fixed seed")
	}
}

func TestThemesVariesAcrossSeeds(t *testing.T) {
	tracks := themedTracks(60)
	seen := make(map[string]struct{})
	for _, seed := range []int64{1, 2, 3, 4, 5} {
		ordered, _ := NewThemesSorter().Sort(WithSeed(context.Background(), seed), tracks)
		seen[titlesOf(ordered)] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatal("expected different chaptering across seeds")
	}
}

// TestThemesChaptersBoundedAndBuild locks the non-negotiables: every chapter is at
// most themeMaxMinutes long and its intensity is non-decreasing (it builds).
func TestThemesChaptersBoundedAndBuild(t *testing.T) {
	tracks := themedTracks(80)
	rng := rand.New(rand.NewSource(42))
	chapters := composeChapters(tracks, rng)
	if len(chapters) < 2 {
		t.Fatalf("expected multiple chapters, got %d", len(chapters))
	}

	total := 0
	for c, ch := range chapters {
		total += len(ch)
		if len(ch) > themeMaxTracks {
			t.Fatalf("chapter %d has %d tracks, exceeds cap %d", c, len(ch), themeMaxTracks)
		}
		mins := 0.0
		for _, tr := range ch {
			if tr.Duration != nil {
				mins += float64(*tr.Duration) / 60.0
			}
		}
		if mins > themeMaxMinutes+1e-9 {
			t.Fatalf("chapter %d is %.1f min, exceeds %.0f", c, mins, themeMaxMinutes)
		}
		for i := 1; i < len(ch); i++ {
			if intensity(ch[i]) < intensity(ch[i-1]) {
				t.Fatalf("chapter %d does not build: intensity dropped at %d", c, i)
			}
		}
	}
	if total != len(tracks) {
		t.Fatalf("chapters cover %d tracks, want %d", total, len(tracks))
	}
}

func TestThemesSmallInputPlacesAll(t *testing.T) {
	tracks := themedTracks(3) // below themeMinChapter
	ordered, err := NewThemesSorter().Sort(WithSeed(context.Background(), 1), tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	if len(ordered) != 3 {
		t.Fatalf("got %d tracks, want 3", len(ordered))
	}
}
