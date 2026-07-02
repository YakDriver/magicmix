package strategy

import (
	"sort"

	"github.com/YakDriver/magicmix/internal/track"
)

// DroppedTrack is a track excluded from a mix because it did not fit, along with the
// roughness (marginal score cost) it contributed.
type DroppedTrack struct {
	Track        track.Track
	MarginalCost float64
}

// outlierMinGain is the absolute roughness a track must add before it is eligible to
// be dropped, so a uniformly good mix is never trimmed just because one track is
// marginally worse than the rest.
const outlierMinGain = 1.0

// TrimOutliers examines an already-ordered mix and removes up to maxFraction of the
// tracks that fit worst — those whose removal most improves the total score and that
// stand out as statistical outliers (Tukey upper fence) above an absolute floor. If
// the collection is coherent, nothing is dropped. Kept tracks are returned in their
// original order; callers typically re-optimize them for a clean final sequence.
func TrimOutliers(ordered []track.Track, maxFraction float64) ([]track.Track, []DroppedTrack) {
	n := len(ordered)
	maxDrop := int(float64(n) * maxFraction)
	if n < 5 || maxDrop < 1 {
		return ordered, nil
	}

	w := DefaultWeights
	base := mixTotal(ordered, w)

	gains := make([]float64, n)
	scratch := make([]track.Track, 0, n-1)
	for i := range ordered {
		scratch = scratch[:0]
		scratch = append(scratch, ordered[:i]...)
		scratch = append(scratch, ordered[i+1:]...)
		gains[i] = base - mixTotal(scratch, w)
	}

	fence := tukeyUpperFence(gains)

	type candidate struct {
		idx  int
		gain float64
	}
	var candidates []candidate
	for i, g := range gains {
		if g > fence && g > outlierMinGain {
			candidates = append(candidates, candidate{i, g})
		}
	}
	sort.SliceStable(candidates, func(a, b int) bool {
		return candidates[a].gain > candidates[b].gain
	})
	if len(candidates) > maxDrop {
		candidates = candidates[:maxDrop]
	}
	if len(candidates) == 0 {
		return ordered, nil
	}

	drop := make(map[int]float64, len(candidates))
	for _, c := range candidates {
		drop[c.idx] = c.gain
	}

	keep := make([]track.Track, 0, n-len(drop))
	dropped := make([]DroppedTrack, 0, len(drop))
	for i, t := range ordered {
		if g, ok := drop[i]; ok {
			dropped = append(dropped, DroppedTrack{Track: t, MarginalCost: g})
			continue
		}
		keep = append(keep, t)
	}
	return keep, dropped
}

// tukeyUpperFence returns Q3 + 1.5*IQR, the classic threshold above which a value is
// considered a high outlier.
func tukeyUpperFence(vals []float64) float64 {
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	q1 := quantileFloats(sorted, 0.25)
	q3 := quantileFloats(sorted, 0.75)
	return q3 + 1.5*(q3-q1)
}
