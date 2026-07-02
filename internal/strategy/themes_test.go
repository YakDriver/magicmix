package strategy

import (
	"context"
	"fmt"
	"testing"

	"github.com/YakDriver/magicmix/internal/track"
)

// themedTracks builds a varied set with valence + energy so multiple pots activate.
func themedTracks(n int) []track.Track {
	keys := []string{"1A", "2A", "3A", "4A", "5A", "8B", "9B", "10B", "11B", "12B"}
	tracks := make([]track.Track, n)
	for i := range tracks {
		v := (i * 37) % 100 // spread valence across the range
		tracks[i] = track.Track{
			Title:   fmt.Sprintf("t%02d", i),
			BPM:     115 + float64(i%15),
			Energy:  20 + (i*13)%80,
			Key:     mustKey(keys[i%len(keys)]),
			Valence: &v,
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
	tracks := themedTracks(40)
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
	tracks := themedTracks(40)
	first, err := NewThemesSorter().Sort(WithSeed(context.Background(), 99), tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	second, err := NewThemesSorter().Sort(WithSeed(context.Background(), 99), tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	if titlesOf(first) != titlesOf(second) {
		t.Fatal("themes must be deterministic for a fixed seed")
	}
}

func TestThemesVariesAcrossSeeds(t *testing.T) {
	tracks := themedTracks(40)
	orderings := make(map[string]struct{})
	for _, seed := range []int64{1, 2, 3, 4, 5} {
		ordered, err := NewThemesSorter().Sort(WithSeed(context.Background(), seed), tracks)
		if err != nil {
			t.Fatalf("Sort error: %v", err)
		}
		orderings[titlesOf(ordered)] = struct{}{}
	}
	if len(orderings) < 2 {
		t.Fatal("expected different chaptering across seeds (random pot subsets)")
	}
}

func TestThemesSmallInputFallsBackToFlow(t *testing.T) {
	tracks := themedTracks(3) // below themeMinWaveTracks
	ordered, err := NewThemesSorter().Sort(WithSeed(context.Background(), 1), tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	if len(ordered) != 3 {
		t.Fatalf("got %d tracks, want 3", len(ordered))
	}
}
