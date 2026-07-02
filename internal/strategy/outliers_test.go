package strategy

import (
	"testing"

	"github.com/YakDriver/magicmix/internal/track"
)

func mkTrack(title string, bpm float64, energy int, key string) track.Track {
	k, err := track.ParseKey(key)
	if err != nil {
		panic(err)
	}
	return track.Track{Title: title, BPM: bpm, Energy: energy, Key: k}
}

// a smooth, coherent sequence: adjacent Camelot steps, tight BPM, gentle energy.
func coherentSequence() []track.Track {
	keys := []string{"1A", "2A", "3A", "4A", "5A", "6A", "7A", "8A", "9A", "10A", "11A", "12A", "1A", "2A", "3A"}
	seq := make([]track.Track, len(keys))
	bpm := 120.0
	energy := 40
	for i, k := range keys {
		seq[i] = mkTrack("song", bpm, energy, k)
		bpm += 1
		energy += 3
		if energy > 85 {
			energy = 40 // gentle reset
		}
	}
	return seq
}

func TestTrimOutliersKeepsCoherentSet(t *testing.T) {
	seq := coherentSequence()
	keep, dropped := TrimOutliers(seq, 0.10)
	if len(dropped) != 0 {
		t.Fatalf("expected no drops from a coherent set, dropped %d", len(dropped))
	}
	if len(keep) != len(seq) {
		t.Fatalf("expected all %d tracks kept, got %d", len(seq), len(keep))
	}
}

func TestTrimOutliersDropsMisfit(t *testing.T) {
	seq := coherentSequence()
	// Insert a glaring misfit in the middle: distant key and extreme tempo.
	misfit := mkTrack("MISFIT", 68, 12, "6B")
	withMisfit := make([]track.Track, 0, len(seq)+1)
	withMisfit = append(withMisfit, seq[:7]...)
	withMisfit = append(withMisfit, misfit)
	withMisfit = append(withMisfit, seq[7:]...)

	keep, dropped := TrimOutliers(withMisfit, 0.10)
	if len(dropped) == 0 {
		t.Fatal("expected the misfit to be dropped")
	}
	for _, d := range dropped {
		if d.Track.Title != "MISFIT" {
			t.Fatalf("dropped the wrong track: %q", d.Track.Title)
		}
	}
	for _, k := range keep {
		if k.Title == "MISFIT" {
			t.Fatal("misfit should not remain in kept set")
		}
	}
}

func TestTrimOutliersRespectsFractionCap(t *testing.T) {
	// With 15 tracks, a 10% cap allows at most 1 drop even if more look rough.
	seq := coherentSequence()
	seq[3] = mkTrack("BAD1", 70, 10, "6B")
	seq[9] = mkTrack("BAD2", 200, 95, "7B")
	_, dropped := TrimOutliers(seq, 0.10)
	if len(dropped) > 1 {
		t.Fatalf("10%% of 15 tracks caps drops at 1, got %d", len(dropped))
	}
}
