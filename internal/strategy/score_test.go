package strategy

import (
	"math"
	"testing"

	"github.com/YakDriver/magicmix/internal/track"
)

// TestContourRewardsWavesNotRamps is the crux: two clean builds separated by a reset
// (the user's example) must NOT be penalized, while jittery and monotonic shapes are.
func TestContourRewardsWavesNotRamps(t *testing.T) {
	twoRamps := []float64{70, 75, 80, 85, 90, 50, 60, 70, 80} // build, reset, build
	zigzag := []float64{70, 90, 60, 95, 55, 100, 50}          // choppy, no real builds

	twoRampsCost := contourPenalty(twoRamps).RawPenalty
	zigzagCost := contourPenalty(zigzag).RawPenalty

	if twoRampsCost > 0.01 {
		t.Fatalf("two clean ramps with a reset should be ~free, got %.3f", twoRampsCost)
	}
	if zigzag := zigzagCost; zigzag <= twoRampsCost {
		t.Fatalf("zigzag (%.3f) should cost more than two clean ramps (%.3f)", zigzag, twoRampsCost)
	}
}

func TestContourPenalizesMonotony(t *testing.T) {
	// A long single ramp with no resets should be penalized for monotony.
	ramp := make([]float64, 60)
	for i := range ramp {
		ramp[i] = 40 + float64(i)*2 // +2 per step, one endless build
	}
	stats := contourPenalty(ramp)
	if stats.Resets != 0 {
		t.Fatalf("expected 0 resets in a monotonic ramp, got %d", stats.Resets)
	}
	if stats.WaveCountPenalty <= 0 {
		t.Fatalf("expected a monotony (wave-count) penalty for one long ramp, got %.3f", stats.WaveCountPenalty)
	}
}

func TestContourResetMustFollowRealBuild(t *testing.T) {
	// A drop right after a one-track "build" is jitter, not a reset, and is penalized.
	jitter := []float64{50, 90, 40}            // up then immediate crash: no qualifying build
	realReset := []float64{50, 60, 70, 80, 40} // qualifying build then reset

	if contourPenalty(jitter).ResetPenalty <= 0 {
		t.Fatal("a reset after a non-build should incur a jitter penalty")
	}
	if p := contourPenalty(realReset).ResetPenalty; p != 0 {
		t.Fatalf("a reset after a qualifying build should be free, got %.3f", p)
	}
}

// TestScoreMatchesFlowObjective guarantees the optimizer minimizes exactly what the
// report measures: flow's pathCost equals ScoreMix.Total for the same ordering.
func TestScoreMatchesFlowObjective(t *testing.T) {
	seq := flowTestTracks()
	w := DefaultWeights

	cm := buildCostMatrix(seq, w)
	perm := make([]int, len(seq))
	for i := range perm {
		perm[i] = i
	}

	got := cm.pathCost(perm)
	want := ScoreMixWith(seq, w).Total
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("flow objective (%.6f) must equal ScoreMix.Total (%.6f)", got, want)
	}
}

func TestScoreAdaptsToAvailableSignals(t *testing.T) {
	base := flowTestTracks() // only key/bpm/energy
	got := ScoreMix(base).ActiveSignals
	want := map[string]bool{"harmonic": true, "tempo": true, "energy": true}
	if len(got) != 3 {
		t.Fatalf("expected 3 active signals for key/bpm/energy data, got %v", got)
	}
	for _, s := range got {
		if !want[s] {
			t.Fatalf("unexpected active signal %q", s)
		}
	}

	withValence := make([]track.Track, len(base))
	copy(withValence, base)
	for i := range withValence {
		v := 50
		withValence[i].Valence = &v
	}
	if !containsSignal(ScoreMix(withValence).ActiveSignals, "valence") {
		t.Fatal("valence should be active once present on most tracks")
	}
}

func containsSignal(signals []string, want string) bool {
	for _, s := range signals {
		if s == want {
			return true
		}
	}
	return false
}
