package strategy_test

import (
	"context"
	"testing"

	"github.com/YakDriver/magicmix/internal/strategy"
	"github.com/YakDriver/magicmix/internal/track"
)

func TestDefaultSorterCamelotProgression(t *testing.T) {
	t.Helper()
	tracks := sampleTracks(t)

	sorter := strategy.NewDefaultSorter()

	ctx := strategy.WithSeed(context.Background(), 12345)

	ordered, err := sorter.Sort(ctx, cloneTracks(tracks))
	if err != nil {
		t.Fatalf("Sort returned error: %v", err)
	}
	if len(ordered) != len(tracks) {
		t.Fatalf("Sort returned %d tracks, want %d", len(ordered), len(tracks))
	}

	for i, tr := range ordered {
		t.Logf("%02d: %s | %s | %.1f BPM | %d Energy", i, tr.Key.String(), tr.Title, tr.BPM, tr.Energy)
	}

	// Sorting should be deterministic.
	ordered2, err := sorter.Sort(ctx, cloneTracks(tracks))
	if err != nil {
		t.Fatalf("second Sort error: %v", err)
	}
	for i := range ordered {
		if ordered[i].Title != ordered2[i].Title {
			t.Fatalf("non-deterministic sorting at index %d: %q vs %q", i, ordered[i].Title, ordered2[i].Title)
		}
	}

	var maxBPMJump float64
	energyDrops := 0
	var currentEnergyRun int

	remaining := cloneTracks(tracks)
	removeTrack(t, &remaining, ordered[0])

	for i := 1; i < len(ordered); i++ {
		prev := ordered[i-1]
		next := ordered[i]

		diff := next.Key.Number - prev.Key.Number
		if diff < 0 {
			diff += 12
		}

		if diff > 3 {
			if hasPreferredCandidate(prev, remaining) {
				t.Fatalf("key step too large between %s (%s) and %s (%s) despite preferred options", prev.Title, prev.Key.String(), next.Title, next.Key.String())
			}
		}
		if diff > 0 && next.Key.Mode != prev.Key.Mode {
			if hasPreferredCandidate(prev, remaining) {
				t.Fatalf("invalid key change between %s (%s) and %s (%s) despite preferred options", prev.Title, prev.Key.String(), next.Title, next.Key.String())
			}
		}

		bpmDiff := next.BPM - prev.BPM
		if bpmDiff < 0 {
			bpmDiff = -bpmDiff
		}
		if bpmDiff > maxBPMJump {
			maxBPMJump = bpmDiff
		}

		if next.Energy < prev.Energy {
			if prev.Energy-next.Energy >= 10 {
				energyDrops++
			}
			currentEnergyRun = 0
		} else {
			currentEnergyRun++
		}

		removeTrack(t, &remaining, next)
	}

	if maxBPMJump > 6.0 {
		t.Fatalf("expected to avoid large bpm jumps, saw %.1f", maxBPMJump)
	}
	if energyDrops == 0 {
		t.Fatalf("expected at least one energy reset to form cycles")
	}
}

func sampleTracks(t *testing.T) []track.Track {
	t.Helper()
	rows := []struct {
		title  string
		artist string
		bpm    float64
		energy int
		key    string
	}{
		{"Just A Gigolo - Remastered", "Louis Prima", 125, 40, "4B"},
		{"Praise (Radio Version)", "Elevation Worship", 127, 79, "4B"},
		{"Leave Town for a Week", "Leo Dijon", 124, 53, "7B"},
		{"Falling", "Trevor Daniel", 127, 43, "3A"},
		{"End of Time (VIZE Remix)", "K-391", 126, 51, "9A"},
		{"Stay", "Kaivon", 123, 73, "3B"},
		{"High Above", "INZO", 128, 60, "11B"},
		{"He Deserves", "Tedashii", 125, 49, "9B"},
		{"Lovers", "Solarstone", 128, 55, "6B"},
		{"All The Roads", "Mango", 123, 66, "3B"},
		{"fault-line", "Le Youth", 124, 76, "8B"},
		{"Good Old Days (feat. Kesha)", "Macklemore", 123, 51, "4B"},
		{"Draiocht", "GAR", 123, 74, "10B"},
		{"CUT ME OUT", "Rezz", 125, 74, "3B"},
		{"Conundrum", "EDX", 124, 78, "8B"},
		{"Heaven Takes You Home (feat. Connie Constance)", "Swedish House Mafia", 125, 74, "5A"},
	}

	tracks := make([]track.Track, len(rows))
	for i, row := range rows {
		key, err := track.ParseKey(row.key)
		if err != nil {
			t.Fatalf("ParseKey(%s): %v", row.key, err)
		}
		tracks[i] = track.Track{
			Title:  row.title,
			Artist: row.artist,
			BPM:    row.bpm,
			Energy: row.energy,
			Key:    key,
		}
	}
	return tracks
}

func cloneTracks(tracks []track.Track) []track.Track {
	out := make([]track.Track, len(tracks))
	copy(out, tracks)
	return out
}

func removeTrack(t *testing.T, tracks *[]track.Track, target track.Track) {
	t.Helper()
	slice := *tracks
	for i, cand := range slice {
		if equalTrack(cand, target) {
			last := len(slice) - 1
			slice[i] = slice[last]
			*tracks = slice[:last]
			return
		}
	}
	t.Fatalf("track %+v not found in remaining set", target)
}

func equalTrack(a, b track.Track) bool {
	return a.Title == b.Title && a.Artist == b.Artist &&
		a.BPM == b.BPM && a.Energy == b.Energy &&
		a.Key == b.Key
}

func hasPreferredCandidate(prev track.Track, remaining []track.Track) bool {
	for _, cand := range remaining {
		if cand.Title == prev.Title && cand.Artist == prev.Artist {
			continue
		}
		diff := cand.Key.Number - prev.Key.Number
		if diff < 0 {
			diff += 12
		}
		if diff == 0 {
			return true
		}
		if diff <= 3 && cand.Key.Mode == prev.Key.Mode {
			return true
		}
	}
	return false
}
