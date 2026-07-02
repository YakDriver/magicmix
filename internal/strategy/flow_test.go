package strategy

import (
	"context"
	"math"
	"testing"

	"github.com/YakDriver/magicmix/internal/track"
)

func intPtr(v int) *int { return &v }

func flowTestTracks() []track.Track {
	rows := []struct {
		title  string
		bpm    float64
		energy int
		key    string
	}{
		{"a", 120, 40, "8A"}, {"b", 122, 45, "9A"}, {"c", 124, 55, "9B"},
		{"d", 126, 60, "10B"}, {"e", 128, 70, "11B"}, {"f", 121, 42, "8B"},
		{"g", 96, 30, "3A"}, {"h", 100, 35, "3B"}, {"i", 130, 80, "12B"},
		{"j", 118, 50, "7B"},
	}
	tracks := make([]track.Track, len(rows))
	for i, r := range rows {
		key, err := track.ParseKey(r.key)
		if err != nil {
			panic(err)
		}
		tracks[i] = track.Track{Title: r.title, BPM: r.bpm, Energy: r.energy, Key: key}
	}
	return tracks
}

func TestFlowHandlesSmallInputs(t *testing.T) {
	// Regression: chooseStarts must not loop forever when there are fewer than the
	// desired number of random starts (n < 6).
	base := flowTestTracks()
	for _, n := range []int{3, 4, 5} {
		ordered, err := NewFlowSorter().Sort(WithSeed(context.Background(), 1), base[:n])
		if err != nil {
			t.Fatalf("n=%d: Sort error: %v", n, err)
		}
		if len(ordered) != n {
			t.Fatalf("n=%d: got %d tracks", n, len(ordered))
		}
	}
}

func TestFlowSorterDeterministic(t *testing.T) {
	tracks := flowTestTracks()
	sorter := NewFlowSorter()
	ctx := WithSeed(context.Background(), 12345)

	first, err := sorter.Sort(ctx, tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	second, err := sorter.Sort(ctx, tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	for i := range first {
		if first[i].Title != second[i].Title {
			t.Fatalf("non-deterministic at %d: %q vs %q", i, first[i].Title, second[i].Title)
		}
	}
}

func TestFlowSorterPreservesTracks(t *testing.T) {
	tracks := flowTestTracks()
	sorter := NewFlowSorter()
	ordered, err := sorter.Sort(WithSeed(context.Background(), 7), tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	if len(ordered) != len(tracks) {
		t.Fatalf("got %d tracks, want %d", len(ordered), len(tracks))
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

func TestFlowSorterBeatsInputOrder(t *testing.T) {
	tracks := flowTestTracks()
	sorter := NewFlowSorter()
	ordered, err := sorter.Sort(WithSeed(context.Background(), 1), tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	cm := buildCostMatrix(tracks, DefaultWeights)

	inputPerm := make([]int, len(tracks))
	for i := range inputPerm {
		inputPerm[i] = i
	}
	index := make(map[string]int, len(tracks))
	for i, tr := range tracks {
		index[tr.Title] = i
	}
	orderedPerm := make([]int, len(ordered))
	for i, tr := range ordered {
		orderedPerm[i] = index[tr.Title]
	}

	if cm.pathCost(orderedPerm) >= cm.pathCost(inputPerm) {
		t.Fatalf("flow ordering (%.3f) not better than input order (%.3f)",
			cm.pathCost(orderedPerm), cm.pathCost(inputPerm))
	}
}

func TestFlowHandlesMissingOptionalSignals(t *testing.T) {
	// No valence/danceability/acousticness set; must not panic and must order all.
	tracks := flowTestTracks()
	sorter := NewFlowSorter()
	ordered, err := sorter.Sort(WithSeed(context.Background(), 3), tracks)
	if err != nil {
		t.Fatalf("Sort error: %v", err)
	}
	if len(ordered) != len(tracks) {
		t.Fatalf("got %d tracks, want %d", len(ordered), len(tracks))
	}
}

func TestTempoCostOctaveFolding(t *testing.T) {
	// Double-time should be treated as ~free; a small real change should be cheap;
	// a tritone-tempo change should be the most expensive.
	if c := tempoCost(120, 240); c > 1e-9 {
		t.Fatalf("double-time cost = %.4f, want ~0", c)
	}
	if c := tempoCost(120, 128); c >= tempoCost(120, 150) {
		t.Fatalf("expected small tempo change cheaper than large one")
	}
}

func TestMoodAndAcousticCostSkippedWhenAbsent(t *testing.T) {
	a := track.Track{Energy: 50}
	b := track.Track{Energy: 55}
	if valenceCost(a, b) != 0 || acousticCost(a, b) != 0 {
		t.Fatal("optional-signal costs should be zero when signals are absent")
	}
	a.Valence, b.Valence = intPtr(20), intPtr(90)
	if valenceCost(a, b) <= 0 {
		t.Fatal("valence swing should incur mood cost when present")
	}
	if math.Abs(valenceCost(a, b)-valenceCost(b, a)) > 1e-9 {
		t.Fatal("mood cost should be symmetric")
	}
}
