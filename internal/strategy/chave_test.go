package strategy

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/YakDriver/magicmix/internal/track"
)

// chaveTracks builds a varied set (multiple signals + ~3-min durations) so many
// multi-signal groups exist and time-based chave sizing applies.
func chaveTracks(n int) []track.Track {
	keys := []string{"1A", "2A", "3A", "4A", "5A", "8B", "9B", "10B", "11B", "12B"}
	tracks := make([]track.Track, n)
	for i := range tracks {
		v := (i * 37) % 100
		d := (i * 53) % 100
		a := (i * 17) % 100
		p := 50 + (i*29)%50
		dur := 170 + (i*7)%40
		yr := 1985 + (i*11)%41
		tracks[i] = track.Track{
			Title:        fmt.Sprintf("t%02d", i),
			BPM:          100 + float64(i%40),
			Energy:       20 + (i*13)%80,
			Key:          mustKey(keys[i%len(keys)]),
			Valence:      &v,
			Danceability: &d,
			Acousticness: &a,
			Popularity:   &p,
			Duration:     &dur,
			Year:         &yr,
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
	var b []byte
	for _, t := range tracks {
		b = append(b, t.Title...)
		b = append(b, '|')
	}
	return string(b)
}

func TestChavePlacesEveryTrack(t *testing.T) {
	tracks := chaveTracks(80)
	ordered, err := NewChaveSorter().Sort(WithSeed(context.Background(), 7), tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	if len(ordered) != len(tracks) {
		t.Fatalf("placed %d tracks, want %d", len(ordered), len(tracks))
	}
	seen := make(map[string]int)
	for _, tr := range ordered {
		seen[tr.Title]++
	}
	for _, tr := range tracks {
		if seen[tr.Title] != 1 {
			t.Fatalf("track %q appears %d times, want 1", tr.Title, seen[tr.Title])
		}
	}
}

func TestChaveDeterministicPerSeed(t *testing.T) {
	tracks := chaveTracks(80)
	first, _ := NewChaveSorter().Sort(WithSeed(context.Background(), 99), tracks)
	second, _ := NewChaveSorter().Sort(WithSeed(context.Background(), 99), tracks)
	if titlesOf(first) != titlesOf(second) {
		t.Fatal("chave must be deterministic for a fixed seed")
	}
}

func TestChaveVariesAcrossSeeds(t *testing.T) {
	tracks := chaveTracks(80)
	seen := make(map[string]struct{})
	for _, seed := range []int64{1, 2, 3, 4, 5} {
		ordered, _ := NewChaveSorter().Sort(WithSeed(context.Background(), seed), tracks)
		seen[titlesOf(ordered)] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatal("expected different chaving across seeds")
	}
}

// TestChaveBoundedAndBuild locks the non-negotiables: every chave is at most
// chaveMaxMinutes and builds in intensity (second half >= first half).
func TestChaveBoundedAndBuild(t *testing.T) {
	tracks := chaveTracks(90)
	rng := rand.New(rand.NewSource(42))
	chaves, err := composeChaves(context.Background(), tracks, rng)
	if err != nil {
		t.Fatalf("composeChaves error: %v", err)
	}
	if len(chaves) < 2 {
		t.Fatalf("expected multiple chaves, got %d", len(chaves))
	}

	total := 0
	for c, ch := range chaves {
		total += len(ch)
		if len(ch) > chaveMaxTracks {
			t.Fatalf("chave %d has %d tracks, exceeds cap %d", c, len(ch), chaveMaxTracks)
		}
		mins := 0.0
		for _, tr := range ch {
			if tr.Duration != nil {
				mins += float64(*tr.Duration) / 60.0
			}
		}
		if mins > chaveMaxMinutes+1e-9 {
			t.Fatalf("chave %d is %.1f min, exceeds %.0f", c, mins, chaveMaxMinutes)
		}
		if !buildsWell(ch) {
			t.Fatalf("chave %d does not build in intensity", c)
		}
	}
	if total != len(tracks) {
		t.Fatalf("chaves cover %d tracks, want %d", total, len(tracks))
	}
}

func TestChaveRespectsCapWithLongTracks(t *testing.T) {
	// Long songs must not let the min-size preference blow past the 30-min ceiling.
	tracks := chaveTracks(60)
	for i := range tracks {
		d := 480 // 8 minutes each
		tracks[i].Duration = &d
	}
	rng := rand.New(rand.NewSource(3))
	chaves, err := composeChaves(context.Background(), tracks, rng)
	if err != nil {
		t.Fatalf("composeChaves error: %v", err)
	}
	for c, ch := range chaves {
		mins := 0.0
		for _, tr := range ch {
			mins += float64(*tr.Duration) / 60.0
		}
		if mins > chaveMaxMinutes+1e-9 {
			t.Fatalf("chave %d is %.1f min with long tracks, exceeds %.0f", c, mins, chaveMaxMinutes)
		}
	}
}

func TestChaveSmallInputPlacesAll(t *testing.T) {
	tracks := chaveTracks(3)
	ordered, err := NewChaveSorter().Sort(WithSeed(context.Background(), 1), tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	if len(ordered) != 3 {
		t.Fatalf("got %d tracks, want 3", len(ordered))
	}
}
