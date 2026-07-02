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

// FlowSorter treats ordering as a path-optimization problem. It builds an
// asymmetric transition-cost matrix over all tracks, seeds a greedy path, then
// improves it with 2-opt and or-opt local search. Compared to a single-pass greedy
// selector this avoids the "least-bad-options" trap where a cheap early pick forces
// poor transitions later.
//
// The transition cost blends five signals:
//   - harmonic Camelot compatibility (clockwise steps favored over counter-clockwise),
//   - octave-folded tempo distance (so half/double-time pairs are treated as close),
//   - an asymmetric energy/danceability arc (gentle rises cheap, big drops costly),
//   - valence (mood) continuity,
//   - acousticness continuity.
//
// Optional signals (valence, danceability, acousticness) are skipped when the source
// data does not provide them.
type FlowSorter struct {
	weights flowWeights
}

func NewFlowSorter() *FlowSorter {
	return &FlowSorter{weights: defaultFlowWeights}
}

func (s *FlowSorter) Name() string {
	return flowStrategyName
}

type flowWeights struct {
	harmonic float64
	tempo    float64
	energy   float64
	mood     float64
	acoustic float64
}

var defaultFlowWeights = flowWeights{
	harmonic: 1.0,
	tempo:    1.0,
	energy:   0.8,
	mood:     0.4,
	acoustic: 0.2,
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

// costMatrix holds the asymmetric transition cost between every ordered pair.
type costMatrix struct {
	n int
	m []float64 // row-major: cost from i to j at m[i*n+j]
}

func buildCostMatrix(seq []track.Track, w flowWeights) *costMatrix {
	n := len(seq)
	cm := &costMatrix{n: n, m: make([]float64, n*n)}
	for i := range seq {
		for j := range seq {
			if i != j {
				cm.m[i*n+j] = transitionCost(seq[i], seq[j], w)
			}
		}
	}
	return cm
}

func (cm *costMatrix) cost(i, j int) float64 { return cm.m[i*cm.n+j] }

func (cm *costMatrix) pathCost(perm []int) float64 {
	total := 0.0
	for k := 0; k+1 < len(perm); k++ {
		total += cm.cost(perm[k], perm[k+1])
	}
	return total
}

// bestGreedy runs nearest-neighbor from each candidate start and keeps the cheapest.
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

const improvementEps = 1e-9

// localSearch improves perm with 2-opt (segment reversal) and or-opt (segment
// relocation, lengths 1-3) until a full pass yields no improvement. Because every
// accepted move strictly lowers total cost, termination is guaranteed.
func (cm *costMatrix) localSearch(ctx context.Context, perm []int) ([]int, error) {
	cost := cm.pathCost(perm)
	scratch := make([]int, len(perm))

	for {
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
	for len(starts) < 6 {
		add(rng.Intn(n))
	}
	return starts
}

// transitionCost is the asymmetric cost of playing b immediately after a.
func transitionCost(a, b track.Track, w flowWeights) float64 {
	return w.harmonic*harmonicCost(a.Key, b.Key) +
		w.tempo*tempoCost(a.BPM, b.BPM) +
		w.energy*energyCost(intensity(a), intensity(b)) +
		w.mood*moodCost(a, b) +
		w.acoustic*acousticCost(a, b)
}

// harmonicCost scores Camelot compatibility. Clockwise steps (+1, +2) are favored
// over the counter-clockwise (-1) move, matching standard harmonic-mixing practice.
func harmonicCost(a, b track.Key) float64 {
	d := b.Number - a.Number
	for d > 6 {
		d -= 12
	}
	for d < -6 {
		d += 12
	}
	modeChange := a.Mode != b.Mode

	switch {
	case d == 0 && !modeChange:
		return 0.0
	case d == 1 && !modeChange:
		return 0.05
	case d == 0 && modeChange:
		return 0.15 // relative major/minor
	case d == 2 && !modeChange:
		return 0.15 // energy-boost mix
	case d == -1 && !modeChange:
		return 0.20 // subdominant, slightly rougher than +1
	case d == 3 && !modeChange:
		return 0.40
	case d == 4 && !modeChange:
		return 0.55
	case (d == 1 || d == -1) && modeChange:
		return 0.50
	default:
		ad := d
		if ad < 0 {
			ad = -ad
		}
		base := 0.55 + 0.10*float64(ad-1)
		if modeChange {
			base += 0.15
		}
		return math.Min(1.3, base)
	}
}

// tempoCost folds tempo onto the octave circle so half/double-time pairs are cheap,
// then costs the residual percentage difference.
func tempoCost(a, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	octaves := math.Log2(b / a)
	octaves -= math.Round(octaves) // fold to [-0.5, 0.5]
	pctDiff := math.Abs(math.Exp2(octaves)-1) * 100
	return math.Min(1.5, pctDiff/10.0)
}

// energyCost shapes the arc: a gentle rise is ideal, plateaus are fine, large upward
// leaps are mildly jarring, and drops are increasingly costly (a small breather is
// tolerated). The asymmetry produces an overall upward drift with small dips.
func energyCost(from, to float64) float64 {
	d := to - from
	switch {
	case d >= 3 && d <= 15:
		return 0.0
	case d > 15 && d <= 30:
		return (d - 15) / 50.0
	case d > 30:
		return 0.30 + (d-30)/120.0
	case d < 3 && d >= -4:
		return 0.08
	case d < -4 && d >= -12:
		return 0.20 + (-d-4)/40.0
	default:
		return math.Min(1.3, 0.40+(-d-12)/40.0)
	}
}

// moodCost penalizes large valence swings (emotional whiplash) when both known.
func moodCost(a, b track.Track) float64 {
	if a.Valence == nil || b.Valence == nil {
		return 0
	}
	return math.Min(0.8, math.Abs(float64(*a.Valence-*b.Valence))/50.0)
}

// acousticCost penalizes abrupt acoustic/electronic swaps when both known.
func acousticCost(a, b track.Track) float64 {
	if a.Acousticness == nil || b.Acousticness == nil {
		return 0
	}
	return math.Min(0.8, math.Abs(float64(*a.Acousticness-*b.Acousticness))/70.0)
}

// intensity blends energy with danceability (when present) into the value the energy
// arc is shaped around.
func intensity(t track.Track) float64 {
	e := float64(t.Energy)
	if t.Danceability != nil {
		return 0.7*e + 0.3*float64(*t.Danceability)
	}
	return e
}
