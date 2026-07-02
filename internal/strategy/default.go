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
	targetIntervals    map[int]float64 // Target spacing for each key number
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
	keyNumberRunLength   int // New: tracks consecutive tracks with same key number (7A->7B->7A counts as 3)
	stepsSinceStep1      int
	stepsSinceStep2      int
	stepsSinceLetterFlip int
	stepsSinceEnergyDrop int
	stepsSinceKeyNumber  map[int]int // How many tracks since we last used each key number
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

	// Calculate target intervals for optimal distribution
	targetIntervals := make(map[int]float64)
	totalTracks := float64(len(tracks))
	for keyNum, count := range countsByNumber {
		if count > 0 {
			// Target interval = total tracks / count of this key
			// e.g., 158 tracks with 29 10A/10B = every 5.4 tracks
			targetIntervals[keyNum] = totalTracks / float64(count)
		}
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
		targetIntervals:    targetIntervals,
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
	return min(max(int(math.Round(float64(total)/float64(estimatedCycles))), cycleMinTracks), cycleMaxTracks)
}

func (p *mixPlanner) initialState(start track.Track) mixState {
	// Initialize tracking for all key numbers
	stepsSinceKeyNumber := make(map[int]int)
	for keyNum := range p.countsByNumber {
		stepsSinceKeyNumber[keyNum] = 0
	}
	// Set the starting key to have been used
	stepsSinceKeyNumber[start.Key.Number] = 0

	state := mixState{
		prev:                 start,
		prevSet:              true,
		cycleIndex:           0,
		tracksInCycle:        1,
		cycleStartEnergy:     float64(start.Energy),
		desiredCycleLen:      p.desiredCycleLength,
		stats:                p.stats,
		sameNumberStreak:     0,
		keyNumberRunLength:   1, // First track starts the run
		stepsSinceStep1:      0,
		stepsSinceStep2:      0,
		stepsSinceLetterFlip: 0,
		stepsSinceEnergyDrop: 0,
		stepsSinceKeyNumber:  stepsSinceKeyNumber,
	}
	return state
}

func (p *mixPlanner) chooseStartIndex() int {
	// First, collect all tracks grouped by key number to ensure we have good options
	keyGroups := make(map[int][]int)
	for idx, candidate := range p.remaining {
		keyGroups[candidate.Key.Number] = append(keyGroups[candidate.Key.Number], idx)
	}

	// Calculate overrepresentation - if any key is more than 12% of total, prioritize it for starting
	originalTotal := float64(p.totalTracks)
	var overrepresentedKeys []int
	var balancedKeys []int

	for keyNum, indices := range keyGroups {
		ratio := float64(len(indices)) / originalTotal
		if ratio > 0.12 && len(indices) >= 2 {
			overrepresentedKeys = append(overrepresentedKeys, keyNum)
		} else if len(indices) >= 2 {
			balancedKeys = append(balancedKeys, keyNum)
		}
	}

	// Prefer starting with overrepresented keys 70% of the time to burn them down early
	var selectedKeyNum int
	if len(overrepresentedKeys) > 0 && p.rng.Float64() < 0.7 {
		selectedKeyNum = overrepresentedKeys[p.rng.Intn(len(overrepresentedKeys))]
	} else if len(balancedKeys) > 0 {
		selectedKeyNum = balancedKeys[p.rng.Intn(len(balancedKeys))]
	} else {
		// Fallback to any key with multiple tracks
		var eligibleKeys []int
		for keyNum, indices := range keyGroups {
			if len(indices) >= 2 {
				eligibleKeys = append(eligibleKeys, keyNum)
			}
		}
		if len(eligibleKeys) > 0 {
			selectedKeyNum = eligibleKeys[p.rng.Intn(len(eligibleKeys))]
		} else {
			// Last resort: any key
			for keyNum := range keyGroups {
				selectedKeyNum = keyNum
				break
			}
		}
	}

	keyNumberCandidates := keyGroups[selectedKeyNum]

	// Now find the best candidate within that key number using the original scoring logic
	bestScore := math.Inf(1)
	var candidates []int

	for _, idx := range keyNumberCandidates {
		candidate := p.remaining[idx]
		score := p.startScoreWithinKey(candidate)
		if score < bestScore-startSelectionTolerance {
			bestScore = score
			candidates = candidates[:0]
			candidates = append(candidates, idx)
		} else if score <= bestScore+startSelectionTolerance {
			candidates = append(candidates, idx)
		}
	}

	if len(candidates) == 0 {
		return keyNumberCandidates[0]
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

func (p *mixPlanner) startScoreWithinKey(candidate track.Track) float64 {
	// Modified scoring function that doesn't heavily favor key frequency
	// Instead focuses on energy and BPM characteristics for good mixing
	energyTarget := p.stats.energyLow
	energyDiff := math.Abs(float64(candidate.Energy) - energyTarget)
	bpmDiff := math.Abs(candidate.BPM - p.stats.bpmMedian)

	// Small bonus for having mode variety
	modeCount := float64(p.countsByKey[candidate.Key])
	modeBonus := 0.0
	if modeCount > 1 {
		modeBonus = -1.0
	}

	return energyDiff*0.4 + bpmDiff*0.1 + modeBonus
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

	// Enhanced key number run penalty system per user requirements
	if state.prevSet && candidate.Key.Number == state.prev.Key.Number {
		runLength := state.keyNumberRunLength + 1
		switch {
		case runLength <= 2:
			// 2 in a row: no problem
		case runLength == 3:
			// 3 in a row: not ideal, small penalty
			total += 8.0
		case runLength == 4:
			// 4 in a row: only if necessary, strong penalty
			total += 30.0
		case runLength == 5:
			// 5 in a row: big problem, "danger" level penalty
			total += 80.0
		default:
			// 6+ in a row: emergency, SEVERE penalty
			total += 200.0 + float64(runLength-5)*50.0
		}
	}

	// REVOLUTIONARY: Proactive variety injection based on remaining inventory
	totalRemaining := float64(len(p.remaining))
	if totalRemaining > 0 && state.prevSet {
		// Calculate mix progress
		originalTotal := float64(p.totalTracks)
		tracksPlayed := originalTotal - totalRemaining
		mixProgress := tracksPlayed / originalTotal

		// Calculate variety opportunity score for this candidate
		varietyScore := calculateVarietyOpportunityScore(p, candidate, mixProgress, totalRemaining)
		total += varietyScore
	}

	// ENHANCED: Sophisticated burn rate management based on position in mix
	originalTotal := float64(p.totalTracks)
	tracksPlayed := originalTotal - float64(len(p.remaining))
	mixProgress := tracksPlayed / originalTotal // 0.0 = start, 1.0 = end

	keyCount := float64(p.countsByNumber[candidate.Key.Number])
	keyInventoryRatio := keyCount / originalTotal

	// Calculate ideal burn rate for this position in the mix
	idealKeysUsedByNow := keyCount * mixProgress
	actualKeysUsed := keyCount - float64(countRemainingForKey(p.remaining, candidate.Key.Number))
	burnRateDeviation := actualKeysUsed - idealKeysUsedByNow

	// Apply position-aware burn rate pressure
	if keyInventoryRatio > 0.15 { // High frequency keys (>15% of mix)
		// MUCH MORE AGGRESSIVE: Force extreme early consumption
		if mixProgress < 0.5 && burnRateDeviation < 0 {
			// We're behind schedule - MASSIVE bonus to force consumption
			burnBonus := (-burnRateDeviation) * 100.0 * (0.5 - mixProgress) * 4.0
			total -= burnBonus
		} else if mixProgress > 0.5 && mixProgress < 0.8 {
			// Middle phase: continue burning pressure
			if burnRateDeviation < -1 {
				burnBonus := (-burnRateDeviation - 1) * 60.0
				total -= burnBonus
			}
		} else if mixProgress > 0.8 {
			// End phase: SEVERELY penalize to force variety
			endGamePenalty := keyInventoryRatio * 200.0 * (mixProgress - 0.8) * 6.0
			total += endGamePenalty
		}
	} else if keyInventoryRatio > 0.10 { // Medium frequency keys (10-15%)
		// Moderate but firm pressure for medium keys
		if mixProgress < 0.4 && burnRateDeviation < -1 {
			burnBonus := (-burnRateDeviation - 1) * 70.0 * (0.4 - mixProgress)
			total -= burnBonus
		} else if mixProgress > 0.75 {
			// Strong late penalty for medium frequency keys
			latePenalty := keyInventoryRatio * 120.0 * (mixProgress - 0.75) * 4.0
			total += latePenalty
		}
	} else if keyInventoryRatio < 0.06 { // Low frequency keys (<6% of mix)
		// Conserve rare keys for strategic placement later
		if mixProgress < 0.4 && actualKeysUsed > idealKeysUsedByNow+1 {
			// Using rare keys too early - penalty
			rarePenalty := (actualKeysUsed - idealKeysUsedByNow - 1) * 40.0
			total += rarePenalty
		} else if mixProgress > 0.6 && burnRateDeviation < -1 {
			// Haven't used enough rare keys yet - moderate bonus
			rareBonus := (-burnRateDeviation - 1) * 15.0
			total -= rareBonus
		}
	}

	// Legacy inventory penalty for backwards compatibility (reduced)
	if keyInventoryRatio > 0.12 {
		remainingRatio := float64(len(p.remaining)) / originalTotal
		urgency := 1.0 - remainingRatio
		inventoryPenalty := (keyInventoryRatio - 0.12) * 15.0 * (1.0 + urgency*1.0)
		total += inventoryPenalty
	}

	// NEW: Smart distribution-based scoring
	if state.prevSet {
		stepsSince := float64(state.stepsSinceKeyNumber[candidate.Key.Number])
		targetInterval := p.targetIntervals[candidate.Key.Number]

		if targetInterval > 0 {
			// Calculate how overdue this key is based on target spacing
			overdueFactor := stepsSince / targetInterval

			if overdueFactor > 1.8 {
				// This key is VERY overdue - give it a large bonus
				bonus := (overdueFactor - 1.8) * 40.0
				total -= bonus
			} else if overdueFactor > 1.2 {
				// This key is overdue - give it a bonus
				bonus := (overdueFactor - 1.2) * 20.0
				total -= bonus
			} else if overdueFactor < 0.4 && candidate.Key.Number == state.prev.Key.Number {
				// This key was just used and is not due yet - strong penalty for consecutive use
				prematurePenalty := (0.4 - overdueFactor) * 60.0
				total += prematurePenalty
			}
		}

		// ENHANCED: Proactive run prevention with lookahead
		if state.keyNumberRunLength >= 3 && candidate.Key.Number != state.prev.Key.Number {
			// Reward breaking out of long runs
			breakoutBonus := float64(state.keyNumberRunLength-2) * 25.0
			total -= breakoutBonus
		}

		// NEW: Predictive run prevention - look ahead to prevent future long runs
		if candidate.Key.Number == state.prev.Key.Number {
			// This would extend the current run - calculate future risk
			futureRunLength := state.keyNumberRunLength + 1
			keysRemainingOfThisType := float64(countRemainingForKey(p.remaining, candidate.Key.Number))

			// If we're at high risk of creating a very long run, severely penalize
			if futureRunLength >= 2 && keysRemainingOfThisType > 5 && mixProgress > 0.7 {
				// Late in the mix with many keys of this type left - HIGH RISK
				runRiskPenalty := keysRemainingOfThisType * 30.0 * (mixProgress - 0.7) * float64(futureRunLength)
				total += runRiskPenalty
			} else if futureRunLength >= 3 && keysRemainingOfThisType > 3 {
				// Medium risk scenario
				runRiskPenalty := keysRemainingOfThisType * 15.0 * float64(futureRunLength-2)
				total += runRiskPenalty
			}
		}

		// NEW: Diversity promotion - prefer keys that create better variety
		if candidate.Key.Number != state.prev.Key.Number {
			// This creates variety - check if we should give it extra credit
			keysOfThisTypeRemaining := float64(countRemainingForKey(p.remaining, candidate.Key.Number))
			totalKeysRemaining := float64(len(p.remaining))

			if keysOfThisTypeRemaining/totalKeysRemaining < 0.15 { // This key type is becoming rare
				// Bonus for using rare keys while we still can
				diversityBonus := (0.15 - keysOfThisTypeRemaining/totalKeysRemaining) * 20.0
				total -= diversityBonus
			}
		}
	}

	return total
}

// countRemainingForKey counts how many tracks of a specific key number remain
func countRemainingForKey(remaining []track.Track, keyNumber int) int {
	count := 0
	for _, t := range remaining {
		if t.Key.Number == keyNumber {
			count++
		}
	}
	return count
}

// calculateVarietyOpportunityScore determines how much to prefer/penalize a candidate
// based on strategic variety management to prevent late-game monotony
func calculateVarietyOpportunityScore(p *mixPlanner, candidate track.Track, mixProgress, totalRemaining float64) float64 {
	keyNumber := candidate.Key.Number
	keyCount := float64(p.countsByNumber[keyNumber])
	originalTotal := float64(p.totalTracks)
	keyInventoryRatio := keyCount / originalTotal

	keysRemainingOfThisType := float64(countRemainingForKey(p.remaining, keyNumber))

	// Strategy 1: Aggressive early burning of high-frequency keys
	if keyInventoryRatio > 0.15 { // High frequency keys like 10A/10B (29 tracks)
		if mixProgress < 0.4 {
			// Very early: moderate pressure to use high-frequency keys
			return -20.0 * (0.4 - mixProgress)
		} else if mixProgress < 0.7 {
			// Middle: strong pressure to burn high-frequency keys
			return -40.0 * (0.7 - mixProgress)
		} else {
			// Late: extreme penalty for high-frequency keys to force variety
			latePenalty := keysRemainingOfThisType * 50.0 * (mixProgress - 0.7) * 3.0
			return latePenalty
		}
	}

	// Strategy 2: Strategic conservation of rare keys
	if keyInventoryRatio < 0.06 { // Rare keys like 5A/5B (4 tracks)
		if mixProgress < 0.3 {
			// Early: strong penalty for using rare keys too soon
			return 50.0 * (0.3 - mixProgress)
		} else if mixProgress > 0.8 && keysRemainingOfThisType > 0 {
			// Very late: bonus for finally using conserved rare keys
			return -30.0 * (mixProgress - 0.8)
		}
	}

	// Strategy 3: Balanced medium-frequency key management
	if keyInventoryRatio >= 0.06 && keyInventoryRatio <= 0.15 {
		// Medium frequency keys - apply proportional pressure
		if mixProgress > 0.6 {
			return keysRemainingOfThisType * 10.0 * (mixProgress - 0.6)
		}
	}

	return 0.0
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
		state.keyNumberRunLength = 1
		state.stepsSinceStep1 = 0
		state.stepsSinceStep2 = 0
		state.stepsSinceLetterFlip = 0
		state.stepsSinceEnergyDrop = 0
		// stepsSinceKeyNumber already initialized in initialState
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

	// Track key number runs (7A->7B->7A = 3 in a row)
	if next.Key.Number == previous.Key.Number {
		state.keyNumberRunLength++
	} else {
		state.keyNumberRunLength = 1
	}

	// Update spacing tracking for all keys
	for keyNum := range state.stepsSinceKeyNumber {
		state.stepsSinceKeyNumber[keyNum]++
	}
	// Reset counter for the key we just used
	state.stepsSinceKeyNumber[next.Key.Number] = 0

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
