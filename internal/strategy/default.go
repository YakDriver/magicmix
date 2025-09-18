package strategy

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/YakDriver/magicmix/internal/track"
)

const (
	defaultStrategyName = "default"

	cycleMinTracks   = 6
	cycleMaxTracks   = 10
	cycleIdealTracks = 8

	keyWeight    = 12.0
	bpmWeight    = 0.8
	energyWeight = 2.0

	varietyStepThreshold    = 2
	varietyStepWeight       = 3.0
	varietyLetterThreshold  = 5
	varietyLetterWeight     = 2.5
	varietyEnergyThreshold  = 5
	varietyEnergyReward     = 1.5
	varietyEnergyPenalty    = 0.6
	energyDropThreshold     = 10
	startSelectionTolerance = 1.0
)

// DefaultSorter applies heuristic ordering to balance key continuity, BPM smoothness,
// and energy cycling while respecting Camelot key constraints.
type DefaultSorter struct{}

func NewDefaultSorter() *DefaultSorter {
	return &DefaultSorter{}
}

func (s *DefaultSorter) Name() string {
	return defaultStrategyName
}

func (s *DefaultSorter) Sort(ctx context.Context, tracks []track.Track) ([]track.Track, error) {
	if len(tracks) <= 1 {
		copied := make([]track.Track, len(tracks))
		for i, t := range tracks {
			copied[i] = t.Clone()
		}
		return copied, nil
	}

	targetCount := len(tracks)
	if limit := limitFromContext(ctx); limit > 0 && limit < targetCount {
		targetCount = limit
	}
	planner := newMixPlanner(ctx, tracks, targetCount)

	ordered := make([]track.Track, 0, targetCount)

	startIdx := planner.chooseStartIndex()
	start := planner.take(startIdx)

	state := planner.initialState(start)
	ordered = append(ordered, start)
	if len(ordered) >= targetCount {
		return ordered, nil
	}

	for planner.remainingCount() > 0 && len(ordered) < targetCount {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		idx := planner.chooseNextIndex(&state)
		next := planner.take(idx)
		state.advance(next)
		ordered = append(ordered, next)
	}

	return ordered, nil
}

// mixPlanner owns the dataset under consideration and tracks remaining inventory.
type mixPlanner struct {
	remaining          []track.Track
	stats              mixStats
	desiredCycleLength int
	countsByKey        map[track.Key]int
	countsByNumber     map[int]int
	rng                *rand.Rand
	totalTracks        int
	targetCount        int
}

type mixStats struct {
	energySorted []int
	bpmSorted    []float64
	energyLow    float64
	energyHigh   float64
	energyMedian float64
	bpmMedian    float64
}

type mixState struct {
	prev                 track.Track
	prevSet              bool
	cycleIndex           int
	tracksInCycle        int
	cycleStartEnergy     float64
	desiredCycleLen      int
	stats                mixStats
	sameNumberStreak     int
	stepsSinceStep1      int
	stepsSinceStep2      int
	stepsSinceLetterFlip int
	stepsSinceEnergyDrop int
}

type transition struct {
	diff       int
	wrap       bool
	modeChange bool
}

func newMixPlanner(ctx context.Context, tracks []track.Track, targetCount int) *mixPlanner {
	remaining := make([]track.Track, len(tracks))
	for i, t := range tracks {
		remaining[i] = t.Clone()
	}

	stats := analyzeMixStats(remaining)

	countsByKey := make(map[track.Key]int, len(remaining))
	countsByNumber := make(map[int]int, len(remaining))
	for _, t := range remaining {
		countsByKey[t.Key]++
		countsByNumber[t.Key.Number]++
	}

	desired := idealCycleLength(len(remaining))

	seed, ok := seedFromContext(ctx)
	if !ok || seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	return &mixPlanner{
		remaining:          remaining,
		stats:              stats,
		desiredCycleLength: desired,
		countsByKey:        countsByKey,
		countsByNumber:     countsByNumber,
		rng:                rng,
		totalTracks:        len(tracks),
		targetCount:        targetCount,
	}
}

func analyzeMixStats(tracks []track.Track) mixStats {
	energies := make([]int, len(tracks))
	bpms := make([]float64, len(tracks))

	for i, t := range tracks {
		energies[i] = t.Energy
		bpms[i] = t.BPM
	}

	sort.Ints(energies)
	sort.Float64s(bpms)

	return mixStats{
		energySorted: energies,
		bpmSorted:    bpms,
		energyLow:    quantileInts(energies, 0.25),
		energyHigh:   quantileInts(energies, 0.75),
		energyMedian: quantileInts(energies, 0.5),
		bpmMedian:    quantileFloats(bpms, 0.5),
	}
}

func idealCycleLength(total int) int {
	if total <= cycleMinTracks {
		if total == 0 {
			return cycleMinTracks
		}
		return total
	}

	estimatedCycles := int(math.Max(1, math.Round(float64(total)/float64(cycleIdealTracks))))
	length := min(max(int(math.Round(float64(total)/float64(estimatedCycles))), cycleMinTracks), cycleMaxTracks)
	return length
}

func (p *mixPlanner) initialState(start track.Track) mixState {
	state := mixState{
		prev:                 start,
		prevSet:              true,
		cycleIndex:           0,
		tracksInCycle:        1,
		cycleStartEnergy:     float64(start.Energy),
		desiredCycleLen:      p.desiredCycleLength,
		stats:                p.stats,
		sameNumberStreak:     0,
		stepsSinceStep1:      0,
		stepsSinceStep2:      0,
		stepsSinceLetterFlip: 0,
		stepsSinceEnergyDrop: 0,
	}
	return state
}

func (p *mixPlanner) chooseStartIndex() int {
	bestScore := math.Inf(1)
	var candidates []int

	for idx, candidate := range p.remaining {
		score := p.startScore(candidate)
		if score < bestScore-startSelectionTolerance {
			bestScore = score
			candidates = candidates[:0]
			candidates = append(candidates, idx)
		} else if score <= bestScore+startSelectionTolerance {
			candidates = append(candidates, idx)
		}
	}

	if len(candidates) == 0 {
		return 0
	}

	return candidates[p.rng.Intn(len(candidates))]
}

func (p *mixPlanner) startScore(candidate track.Track) float64 {
	keyCount := float64(p.countsByNumber[candidate.Key.Number])
	modeCount := float64(p.countsByKey[candidate.Key])

	// Strongly favour starting where we have the deepest inventory so the early cycle can
	// consume the largest clusters before we have to wrap.
	freqScore := -keyCount * 6
	if modeCount > 1 {
		freqScore -= 1
	}

	energyTarget := p.stats.energyLow
	energyDiff := math.Abs(float64(candidate.Energy) - energyTarget)
	bpmDiff := math.Abs(candidate.BPM - p.stats.bpmMedian)

	return freqScore + energyDiff*0.2 + bpmDiff*0.05
}

func (p *mixPlanner) chooseNextIndex(state *mixState) int {
	type choice struct {
		idx   int
		score float64
		set   bool
	}

	var buckets [5]choice

	for idx, candidate := range p.remaining {
		trans := computeTransition(state, candidate)
		score := p.transitionScoreWithTransition(state, candidate, trans)

		category := categorizeTransition(state, trans)
		if category < 0 || category >= len(buckets) {
			continue
		}

		best := &buckets[category]
		if !best.set || score < best.score-1e-6 {
			best.idx = idx
			best.score = score
			best.set = true
		} else if best.set && closeFloat(score, best.score) {
			if p.rng.Intn(2) == 0 {
				best.idx = idx
				best.score = score
			}
		}
	}

	order := categoryOrder(state)
	for _, category := range order {
		if buckets[category].set {
			return buckets[category].idx
		}
	}

	return 0
}

func categoryOrder(state *mixState) []int {
	order := []int{0, 1, 2, 3, 4}
	if state == nil || !state.prevSet {
		return order
	}

	if state.stepsSinceStep2 >= varietyStepThreshold {
		order = []int{1, 0, 2, 3, 4}
	} else if state.stepsSinceStep1 >= varietyStepThreshold {
		order = []int{0, 1, 2, 3, 4}
	}

	if state.stepsSinceLetterFlip >= varietyLetterThreshold {
		order = append([]int{3}, order...)
	}

	seen := make(map[int]struct{}, len(order))
	unique := make([]int, 0, len(order))
	for _, v := range order {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		unique = append(unique, v)
	}
	return unique
}

func (p *mixPlanner) transitionScoreWithTransition(state *mixState, candidate track.Track, trans transition) float64 {
	keyCost := keyTransitionCost(state, trans)
	energyCost := energyTransitionCost(state, candidate, trans, p.stats, p.desiredCycleLength)
	bpmCost := bpmTransitionCost(state, candidate, p.stats)

	remainingCount := float64(p.countsByNumber[candidate.Key.Number])
	flexCost := 0.0
	if remainingCount > 0 {
		flexCost = 1.0 / remainingCount
	}

	total := keyCost*keyWeight + bpmCost*bpmWeight + energyCost*energyWeight + flexCost

	coverage := 1.0
	if p.totalTracks > 0 {
		coverage = float64(p.targetCount) / float64(p.totalTracks)
		if coverage > 1 {
			coverage = 1
		}
		if coverage < 0.1 {
			coverage = 0.1
		}
	}

	if state.prevSet {
		if trans.diff > 0 {
			if remainingCurrent := float64(p.countsByNumber[state.prev.Key.Number]); remainingCurrent > 0 {
				total += remainingCurrent * 0.5 * coverage
			}
			if remainingMode := float64(p.countsByKey[state.prev.Key]); remainingMode > 0 {
				total += remainingMode * 1.0 * coverage
			}
		}

		if trans.diff >= 3 && hasShallowStepOption(state, p, 2) {
			total += float64((trans.diff-2)*8) + 18
		}
	}

	if state.prevSet {
		if trans.diff == 1 {
			if state.stepsSinceStep1 >= varietyStepThreshold {
				bonus := float64(state.stepsSinceStep1-varietyStepThreshold+1) * varietyStepWeight
				total -= bonus
			}
			if state.stepsSinceStep2 >= varietyStepThreshold+1 {
				pen := float64(state.stepsSinceStep2-varietyStepThreshold) * (varietyStepWeight * 0.7)
				total += pen
			}
		}
		if trans.diff == 2 {
			if state.stepsSinceStep2 >= varietyStepThreshold {
				bonus := float64(state.stepsSinceStep2-varietyStepThreshold+1) * varietyStepWeight
				total -= bonus
			}
			if state.stepsSinceStep1 >= varietyStepThreshold+1 {
				pen := float64(state.stepsSinceStep1-varietyStepThreshold) * (varietyStepWeight * 0.7)
				total += pen
			}
		}
		if trans.diff == 0 && trans.modeChange && state.stepsSinceLetterFlip >= varietyLetterThreshold {
			bonus := float64(state.stepsSinceLetterFlip-varietyLetterThreshold+1) * varietyLetterWeight
			total -= bonus
		}
	}

	// Reward moving into a key number that still has plenty of inventory so we consume
	// large clusters sooner.
	baseWeight := 0.6 * coverage
	total -= remainingCount * baseWeight
	total -= float64(p.countsByKey[candidate.Key]) * baseWeight

	// Encourage candidates matching start-of-cycle energy expectations when a wrap is imminent.
	if trans.wrap && trans.diff > 2 && state.tracksInCycle < cycleMinTracks {
		total += 7
	}

	if trans.wrap && trans.diff > 2 {
		// Slight extra penalty for wrapping with large jumps while previous key still has inventory.
		if state.prevSet {
			if remainingCurrent := float64(p.countsByNumber[state.prev.Key.Number]); remainingCurrent > 0 {
				total += remainingCurrent * 2
			}
		}
	}

	if state.prevSet && trans.diff == 0 {
		total += float64(state.sameNumberStreak+1) * 6
	}

	return total
}

func categorizeTransition(state *mixState, trans transition) int {
	if !state.prevSet {
		return 0
	}

	if trans.modeChange && trans.diff > 0 {
		return 4
	}

	switch trans.diff {
	case 1:
		if trans.modeChange {
			return 4
		}
		return 0
	case 2:
		if trans.modeChange {
			return 4
		}
		return 1
	case 0:
		if trans.modeChange {
			return 3
		}
		return 2
	default:
		return 4
	}
}

func hasShallowStepOption(state *mixState, p *mixPlanner, maxStep int) bool {
	if !state.prevSet {
		return true
	}
	for _, cand := range p.remaining {
		trans := computeTransition(state, cand)
		if trans.diff == 0 {
			return true
		}
		if trans.modeChange {
			continue
		}
		if trans.diff > 0 && trans.diff <= maxStep {
			return true
		}
	}
	return false
}

func keyTransitionCost(state *mixState, trans transition) float64 {
	if !state.prevSet {
		return 0
	}

	diff := trans.diff
	cost := 0.0

	switch diff {
	case 0:
		cost = 3.0
	case 1:
		cost = 0
	case 2:
		cost = 0
	case 3:
		cost = 2
	default:
		cost = float64(diff*diff) / 2
	}

	if trans.modeChange && diff > 0 {
		// Strongly discourage changing mode while stepping the number forward, only allow if
		// no alternatives exist.
		cost += 12
	}

	if trans.wrap && diff > 2 {
		// Discourage large downward wraps; relax as the cycle matures or inventory dwindles.
		basePenalty := 6.0
		if state.tracksInCycle >= state.desiredCycleLen-1 {
			basePenalty *= 0.4
		} else if state.tracksInCycle >= state.desiredCycleLen-2 {
			basePenalty *= 0.6
		}
		cost += basePenalty
	}

	return cost
}

func energyTransitionCost(state *mixState, candidate track.Track, trans transition, stats mixStats, desiredCycleLen int) float64 {
	energy := float64(candidate.Energy)

	if !state.prevSet {
		target := stats.energyLow
		return math.Abs(energy-target) / 8
	}

	delta := energy - float64(state.prev.Energy)
	drop := -delta

	if trans.wrap {
		target := stats.energyLow
		cost := math.Abs(energy-target) / 6
		if drop < 12 {
			cost += (12 - drop) / 6
		}
		if drop >= energyDropThreshold && state.stepsSinceEnergyDrop >= varietyEnergyThreshold {
			bonus := float64(state.stepsSinceEnergyDrop-varietyEnergyThreshold+1) * varietyEnergyReward
			cost -= bonus
		} else if drop < energyDropThreshold && state.stepsSinceEnergyDrop >= varietyEnergyThreshold+2 {
			penalty := float64(state.stepsSinceEnergyDrop-(varietyEnergyThreshold+1)) * varietyEnergyPenalty
			cost += penalty
		}
		if cost < -5 {
			cost = -5
		}
		return cost
	}

	target := nextEnergyTarget(state, stats, desiredCycleLen)
	cost := math.Abs(energy-target) / 9

	if delta < 0 {
		if state.tracksInCycle > desiredCycleLen/2 {
			cost += math.Abs(delta) / 10
		} else {
			cost += math.Abs(delta) / 6
		}
	} else if delta > 12 {
		cost += (delta - 12) / 8
	}

	if drop >= energyDropThreshold && state.stepsSinceEnergyDrop >= varietyEnergyThreshold {
		bonus := float64(state.stepsSinceEnergyDrop-varietyEnergyThreshold+1) * varietyEnergyReward
		cost -= bonus
	} else if drop < energyDropThreshold && state.stepsSinceEnergyDrop >= varietyEnergyThreshold+2 {
		penalty := float64(state.stepsSinceEnergyDrop-(varietyEnergyThreshold+1)) * varietyEnergyPenalty
		cost += penalty
	}

	if cost < -5 {
		cost = -5
	}

	return cost
}

func nextEnergyTarget(state *mixState, stats mixStats, desiredCycleLen int) float64 {
	base := state.cycleStartEnergy
	high := stats.energyHigh
	if high <= base {
		high = math.Min(100, base+12)
	}

	progress := float64(state.tracksInCycle) / float64(maxInt(desiredCycleLen, 1))
	if progress > 1 {
		progress = 1
	}

	return base + (high-base)*progress
}

func bpmTransitionCost(state *mixState, candidate track.Track, stats mixStats) float64 {
	if !state.prevSet {
		return math.Abs(candidate.BPM-stats.bpmMedian) / 6
	}

	diff := math.Abs(candidate.BPM - state.prev.BPM)

	if diff <= 1 {
		return diff * 0.2
	}
	if diff <= 2.5 {
		return diff * 0.4
	}
	if diff <= 5 {
		return 0.8 + (diff-2.5)*0.5
	}

	return 2 + (diff-5)*0.7
}

func (p *mixPlanner) take(idx int) track.Track {
	selected := p.remaining[idx]

	// Update counts before removing to ensure the state aligns for future scoring.
	p.countsByKey[selected.Key]--
	if p.countsByKey[selected.Key] <= 0 {
		delete(p.countsByKey, selected.Key)
	}
	p.countsByNumber[selected.Key.Number]--
	if p.countsByNumber[selected.Key.Number] <= 0 {
		delete(p.countsByNumber, selected.Key.Number)
	}

	last := len(p.remaining) - 1
	p.remaining[idx] = p.remaining[last]
	p.remaining = p.remaining[:last]

	return selected
}

func (p *mixPlanner) remainingCount() int {
	return len(p.remaining)
}

func (state *mixState) advance(next track.Track) {
	if !state.prevSet {
		state.prev = next
		state.prevSet = true
		state.cycleStartEnergy = float64(next.Energy)
		state.tracksInCycle = 1
		state.sameNumberStreak = 0
		state.stepsSinceStep1 = 0
		state.stepsSinceStep2 = 0
		state.stepsSinceLetterFlip = 0
		state.stepsSinceEnergyDrop = 0
		return
	}

	previous := state.prev
	trans := computeTransition(state, next)

	state.prev = next
	if trans.wrap {
		state.cycleIndex++
		state.tracksInCycle = 1
		state.cycleStartEnergy = float64(next.Energy)
	} else {
		state.tracksInCycle++
	}

	if trans.diff == 0 {
		state.sameNumberStreak++
	} else {
		state.sameNumberStreak = 0
	}

	cappedIncrement := func(v int) int {
		if v < 100 {
			return v + 1
		}
		return 100
	}

	state.stepsSinceStep1 = cappedIncrement(state.stepsSinceStep1)
	state.stepsSinceStep2 = cappedIncrement(state.stepsSinceStep2)
	state.stepsSinceLetterFlip = cappedIncrement(state.stepsSinceLetterFlip)
	state.stepsSinceEnergyDrop = cappedIncrement(state.stepsSinceEnergyDrop)

	if trans.diff == 1 {
		state.stepsSinceStep1 = 0
	}
	if trans.diff == 2 {
		state.stepsSinceStep2 = 0
	}
	if previous.Key.Mode != next.Key.Mode {
		state.stepsSinceLetterFlip = 0
	}

	energyDelta := float64(next.Energy - previous.Energy)
	if -energyDelta >= energyDropThreshold {
		state.stepsSinceEnergyDrop = 0
	}
}

func computeTransition(state *mixState, candidate track.Track) transition {
	if !state.prevSet {
		return transition{}
	}

	prevNumber := state.prev.Key.Number
	nextNumber := candidate.Key.Number

	diff := nextNumber - prevNumber
	wrap := false
	if diff < 0 {
		diff += 12
		wrap = true
	}

	modeChange := candidate.Key.Mode != state.prev.Key.Mode

	return transition{diff: diff, wrap: wrap, modeChange: modeChange}
}

func quantileInts(values []int, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if q <= 0 {
		return float64(values[0])
	}
	if q >= 1 {
		return float64(values[len(values)-1])
	}

	position := q * float64(len(values)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return float64(values[lower])
	}
	fraction := position - float64(lower)
	return float64(values[lower]) + (float64(values[upper])-float64(values[lower]))*fraction
}

func quantileFloats(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if q <= 0 {
		return values[0]
	}
	if q >= 1 {
		return values[len(values)-1]
	}

	position := q * float64(len(values)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return values[lower]
	}
	fraction := position - float64(lower)
	return values[lower] + (values[upper]-values[lower])*fraction
}

func compareTracks(a, b track.Track) int {
	if a.Key.Number != b.Key.Number {
		return a.Key.Number - b.Key.Number
	}
	if a.Key.Mode != b.Key.Mode {
		if a.Key.Mode < b.Key.Mode {
			return -1
		}
		return 1
	}
	if a.BPM != b.BPM {
		if a.BPM < b.BPM {
			return -1
		}
		return 1
	}
	if a.Energy != b.Energy {
		return a.Energy - b.Energy
	}
	if a.Artist != b.Artist {
		if a.Artist < b.Artist {
			return -1
		}
		return 1
	}
	if a.Title != b.Title {
		if a.Title < b.Title {
			return -1
		}
		return 1
	}
	return 0
}

func closeFloat(a, b float64) bool {
	return math.Abs(a-b) <= 1e-6
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
