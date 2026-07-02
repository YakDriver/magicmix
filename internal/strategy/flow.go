package strategy

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/YakDriver/magicmix/internal/track"
)

const flowStrategyName = "flow"

// FlowSorter treats ordering as a path-optimization problem. It builds a pairwise
// coherence-cost matrix over all tracks, seeds a greedy path from several starts,
// then improves it with 2-opt and or-opt local search. Compared to a single-pass
// greedy selector this avoids the "least-bad-options" trap where a cheap early pick
// forces poor transitions later.
//
// It minimizes exactly the value ScoreMix reports (see score.go): pairwise coherence
// plus the global energy contour. Optimizing the same function we measure is the
// whole point of the shared model.
type FlowSorter struct {
	weights Weights
}

func NewFlowSorter() *FlowSorter {
	return &FlowSorter{weights: DefaultWeights}
}

func (s *FlowSorter) Name() string {
	return flowStrategyName
}

func (s *FlowSorter) Sort(ctx context.Context, tracks []track.Track) ([]track.Track, error) {
	n := len(tracks)
	seq := make([]track.Track, n)
	for i, t := range tracks {
		seq[i] = t.Clone()
	}
	if n <= 2 {
		return seq, nil
	}

	seed, ok := seedFromContext(ctx)
	if !ok || seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	matrix := buildCostMatrix(seq, s.weights)

	bestPerm := matrix.bestGreedy(chooseStarts(seq, rng))
	bestPerm, err := matrix.localSearch(ctx, bestPerm)
	if err != nil {
		return nil, err
	}

	out := make([]track.Track, n)
	for i, idx := range bestPerm {
		out[i] = seq[idx]
	}
	return out, nil
}

// costMatrix caches pairwise coherence costs and the per-track intensity so the full
// score (coherence + global contour) can be evaluated quickly for any permutation.
type costMatrix struct {
	n         int
	m         []float64 // row-major pairwise coherence cost: from i to j at m[i*n+j]
	intens    []float64 // per-track intensity, indexed by track id
	contourW  float64
	minResets int // target wave cadence, invariant to ordering
	maxResets int
	buf       []float64 // reusable scratch for gathering intensities along a permutation
}

func buildCostMatrix(seq []track.Track, w Weights) *costMatrix {
	n := len(seq)
	minResets, maxResets := waveResetBand(seq)
	cm := &costMatrix{
		n:         n,
		m:         make([]float64, n*n),
		intens:    intensities(seq),
		contourW:  w.Contour,
		minResets: minResets,
		maxResets: maxResets,
		buf:       make([]float64, n),
	}
	for i := range seq {
		for j := range seq {
			if i != j {
				cm.m[i*n+j] = coherenceCost(seq[i], seq[j], w)
			}
		}
	}
	return cm
}

func (cm *costMatrix) cost(i, j int) float64 { return cm.m[i*cm.n+j] }

// pathCost returns the full score of a permutation: pairwise coherence plus the
// global contour term. It equals ScoreMixWith(seq, w).Total for the same ordering.
func (cm *costMatrix) pathCost(perm []int) float64 {
	total := 0.0
	for k := 0; k+1 < len(perm); k++ {
		total += cm.cost(perm[k], perm[k+1])
	}
	for i, idx := range perm {
		cm.buf[i] = cm.intens[idx]
	}
	total += cm.contourW * contourPenalty(cm.buf, cm.minResets, cm.maxResets).RawPenalty
	return total
}

// bestGreedy runs nearest-neighbor from each candidate start and keeps the ordering
// with the lowest full score.
func (cm *costMatrix) bestGreedy(starts []int) []int {
	var best []int
	bestCost := math.Inf(1)
	for _, start := range starts {
		perm := cm.greedy(start)
		if c := cm.pathCost(perm); c < bestCost {
			bestCost = c
			best = perm
		}
	}
	return best
}

func (cm *costMatrix) greedy(start int) []int {
	used := make([]bool, cm.n)
	perm := make([]int, 0, cm.n)
	cur := start
	used[cur] = true
	perm = append(perm, cur)
	for len(perm) < cm.n {
		best := -1
		bestCost := math.Inf(1)
		for j := 0; j < cm.n; j++ {
			if used[j] {
				continue
			}
			if c := cm.cost(cur, j); c < bestCost {
				bestCost = c
				best = j
			}
		}
		used[best] = true
		perm = append(perm, best)
		cur = best
	}
	return perm
}

// improvementEps is the smallest score gain worth acting on. It is deliberately well
// above float noise: chasing sub-threshold "improvements" is musically meaningless and
// can make the search run for a very long time on finely-graded cost landscapes.
const improvementEps = 1e-6

// maxLocalSearchPasses caps improvement passes so the search always terminates
// promptly; real inputs converge well within this, and stopping early just leaves a
// near-optimal ordering.
const maxLocalSearchPasses = 60

// localSearch improves perm with 2-opt (segment reversal) and or-opt (segment
// relocation, lengths 1-3) until a full pass yields no improvement or the pass cap is
// reached. Every accepted move lowers total cost by at least improvementEps, so this
// terminates.
func (cm *costMatrix) localSearch(ctx context.Context, perm []int) ([]int, error) {
	cost := cm.pathCost(perm)
	scratch := make([]int, len(perm))

	for pass := 0; pass < maxLocalSearchPasses; pass++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		improved := false

		// 2-opt: reverse perm[i..j].
		for i := 1; i < len(perm)-1; i++ {
			for j := i + 1; j < len(perm); j++ {
				copy(scratch, perm)
				reverseSegment(scratch, i, j)
				if c := cm.pathCost(scratch); c < cost-improvementEps {
					copy(perm, scratch)
					cost = c
					improved = true
				}
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		// or-opt: relocate a segment of length L to another position.
		for l := 1; l <= 3 && l < len(perm); l++ {
			for i := 0; i+l <= len(perm); i++ {
				for p := 0; p <= len(perm)-l; p++ {
					if p == i { // inserting at the original position is a no-op
						continue
					}
					relocateSegment(scratch, perm, i, l, p)
					if c := cm.pathCost(scratch); c < cost-improvementEps {
						copy(perm, scratch)
						cost = c
						improved = true
					}
				}
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		if !improved {
			return perm, nil
		}
	}
	return perm, nil
}

// reverseSegment reverses dst[i..j] inclusive.
func reverseSegment(dst []int, i, j int) {
	for i < j {
		dst[i], dst[j] = dst[j], dst[i]
		i++
		j--
	}
}

// relocateSegment writes into dst the result of removing the length-l segment that
// starts at index i in src and reinserting it so it begins at index p of the
// remaining sequence. dst and src must be distinct slices of equal length.
func relocateSegment(dst, src []int, i, l, p int) {
	seg := make([]int, l)
	copy(seg, src[i:i+l])

	rest := make([]int, 0, len(src)-l)
	rest = append(rest, src[:i]...)
	rest = append(rest, src[i+l:]...)

	// dst = rest[:p] + seg + rest[p:]
	copy(dst, rest[:p])
	copy(dst[p:], seg)
	copy(dst[p+l:], rest[p:])
}

// chooseStarts returns candidate greedy start indices: the calmest tracks (good
// openers to build from) plus a few seed-driven random picks for exploration.
func chooseStarts(seq []track.Track, rng *rand.Rand) []int {
	n := len(seq)
	byIntensity := make([]int, n)
	for i := range byIntensity {
		byIntensity[i] = i
	}
	sort.SliceStable(byIntensity, func(a, b int) bool {
		return intensity(seq[byIntensity[a]]) < intensity(seq[byIntensity[b]])
	})

	seen := make(map[int]struct{})
	starts := make([]int, 0, 6)
	add := func(idx int) {
		if _, ok := seen[idx]; ok {
			return
		}
		seen[idx] = struct{}{}
		starts = append(starts, idx)
	}

	for k := 0; k < 2 && k < n; k++ {
		add(byIntensity[k])
	}
	// Add seed-driven random starts for exploration, but never ask for more unique
	// starts than there are tracks (otherwise this can't be satisfied).
	target := min(6, n)
	for len(starts) < target {
		add(rng.Intn(n))
	}
	return starts
}
