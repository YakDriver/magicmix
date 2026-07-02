package strategy

import (
	"context"
	"math"
	"math/rand"
	"time"

	"github.com/YakDriver/magicmix/internal/track"
)

const (
	constanceStrategyName = "constance"
)

// KeyBucket holds tracks for a specific key (1A-12B)
type KeyBucket struct {
	Key    track.Key
	Tracks []track.Track
}

// ConstanceBuckets manages 24 buckets (12 numbers x 2 modes)
type ConstanceBuckets struct {
	buckets map[track.Key]*KeyBucket
	rng     *rand.Rand
}

// NewConstanceBuckets creates a new bucket system with tracks distributed by key
func NewConstanceBuckets(tracks []track.Track, rng *rand.Rand) *ConstanceBuckets {
	cb := &ConstanceBuckets{
		buckets: make(map[track.Key]*KeyBucket),
		rng:     rng,
	}

	// Initialize all 24 buckets
	for num := 1; num <= 12; num++ {
		for _, mode := range []track.Mode{track.ModeA, track.ModeB} {
			key := track.Key{Number: num, Mode: mode}
			cb.buckets[key] = &KeyBucket{
				Key:    key,
				Tracks: []track.Track{},
			}
		}
	}

	// Distribute tracks into buckets
	for _, t := range tracks {
		bucket, exists := cb.buckets[t.Key]
		if exists {
			bucket.Tracks = append(bucket.Tracks, t.Clone())
		}
	}

	return cb
}

// GetBucketCount returns how many tracks are in a specific key bucket
func (cb *ConstanceBuckets) GetBucketCount(key track.Key) int {
	bucket, exists := cb.buckets[key]
	if !exists {
		return 0
	}
	return len(bucket.Tracks)
}

// GetTrackFromBucket removes and returns the best track from a specific bucket
func (cb *ConstanceBuckets) GetTrackFromBucket(key track.Key, prev track.Track, energyState *EnergyRampState) (track.Track, bool) {
	bucket, exists := cb.buckets[key]
	if !exists || len(bucket.Tracks) == 0 {
		return track.Track{}, false
	}

	// Find best track in this bucket
	bestIdx := 0
	bestScore := cb.scoreCandidateInBucket(bucket.Tracks[0], prev, energyState)

	for i := 1; i < len(bucket.Tracks); i++ {
		score := cb.scoreCandidateInBucket(bucket.Tracks[i], prev, energyState)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	// Remove and return the best track
	bestTrack := bucket.Tracks[bestIdx]
	bucket.Tracks = append(bucket.Tracks[:bestIdx], bucket.Tracks[bestIdx+1:]...)
	return bestTrack, true
}

// scoreCandidateInBucket scores a track within a bucket
func (cb *ConstanceBuckets) scoreCandidateInBucket(candidate, prev track.Track, energyState *EnergyRampState) float64 {
	// BPM compatibility (minimize change)
	bpmDiff := math.Abs(candidate.BPM - prev.BPM)
	bpmScore := math.Max(0, 1.0-bpmDiff/20.0)

	// Energy compatibility based on ramp state
	energyScore := cb.scoreEnergyCompatibilityInBucket(candidate.Energy, prev.Energy, energyState)

	// Combine scores (BPM is priority, then energy)
	return bpmScore*0.7 + energyScore*0.3
}

// scoreEnergyCompatibilityInBucket scores energy transitions for bucket selection
func (cb *ConstanceBuckets) scoreEnergyCompatibilityInBucket(candidateEnergy, prevEnergy int, energyState *EnergyRampState) float64 {
	energyDiff := candidateEnergy - prevEnergy

	if energyState.isBuilding {
		// We want energy to increase (or at least not decrease much)
		if energyDiff > 0 {
			return 1.0 // Perfect - energy increasing
		} else if energyDiff >= -5 {
			return 0.7 // OK - small decrease
		} else {
			return 0.3 // Poor - large decrease when we want to build
		}
	} else {
		// We want energy to decrease for the drop
		if energyDiff < -10 {
			return 1.0 // Perfect - big energy drop
		} else if energyDiff < 0 {
			return 0.8 // Good - some energy drop
		} else if energyDiff <= 5 {
			return 0.5 // OK - stable energy
		} else {
			return 0.2 // Poor - energy increasing when we want a drop
		}
	}
}

// GetTotalTracks returns the total number of tracks remaining across all buckets
func (cb *ConstanceBuckets) GetTotalTracks() int {
	total := 0
	for _, bucket := range cb.buckets {
		total += len(bucket.Tracks)
	}
	return total
}

// ConstanceSorter implements a pattern-based mixing strategy with specific transition rules:
// - 10% sparkle jumps (+1 num, switch letter)
// - 30% same key
// - 60% +1 num, same letter
// - Minimize BPM changes
// - Energy ramps with periodic drops
// Uses buckets to track available tracks by key for smarter decisions
type ConstanceSorter struct{}

func NewConstanceSorter() *ConstanceSorter {
	return &ConstanceSorter{}
}

func (s *ConstanceSorter) Name() string {
	return constanceStrategyName
}

func (s *ConstanceSorter) Sort(ctx context.Context, tracks []track.Track) ([]track.Track, error) {
	if len(tracks) <= 1 {
		copied := make([]track.Track, len(tracks))
		for i, t := range tracks {
			copied[i] = t.Clone()
		}
		return copied, nil
	}

	// Initialize base random number generator
	baseSeed, ok := seedFromContext(ctx)
	if !ok || baseSeed == 0 {
		baseSeed = time.Now().UnixNano()
	}

	// Get target limit from context (if any)
	targetLimit := limitFromContext(ctx)
	if targetLimit <= 0 {
		targetLimit = len(tracks) // Use full list if no limit specified
	}

	// Generate multiple candidate mixes and select the best one
	const numCandidates = 10
	candidates := make([][]track.Track, 0, numCandidates)
	scores := make([]int, 0, numCandidates)

	for candidateNum := 0; candidateNum < numCandidates; candidateNum++ {
		// Use different seed for each candidate to get variety
		candidateSeed := baseSeed + int64(candidateNum*1000)
		candidateMix := s.generateSingleMix(tracks, candidateSeed, targetLimit)

		if len(candidateMix) > 0 {
			// Score this candidate mix with length-quality balance
			compositeScore := s.calculateCompositeScore(candidateMix, len(tracks))

			candidates = append(candidates, candidateMix)
			scores = append(scores, compositeScore)
		}
	}

	// Select the best scoring candidate (lowest composite score wins)
	if len(candidates) == 0 {
		return []track.Track{}, nil // No successful candidates
	}

	bestIdx := 0
	bestScore := scores[0]
	for i := 1; i < len(scores); i++ {
		if scores[i] < bestScore { // Lower composite score is better
			bestScore = scores[i]
			bestIdx = i
		}
	}

	return candidates[bestIdx], nil
}

// calculateCompositeScore balances mix quality with mix length
// We want to reward longer mixes that maintain reasonable quality
// rather than short mixes with perfect scores
func (s *ConstanceSorter) calculateCompositeScore(mix []track.Track, totalInputTracks int) int {
	if len(mix) == 0 {
		return 10000 // Heavily penalize empty mixes
	}

	// Get the raw quality score
	mixScore := ScoreMix(mix)
	qualityScore := mixScore.Total

	// Calculate starting energy adherence bonus/penalty
	startingEnergyScore := 0
	if len(mix) > 0 {
		targetStartingEnergy := 74.0
		actualStartingEnergy := float64(mix[0].Energy)
		energyDiff := math.Abs(actualStartingEnergy - targetStartingEnergy)

		if energyDiff == 0 {
			startingEnergyScore = -50 // 50 point bonus for perfect 74
		} else if energyDiff <= 2 {
			startingEnergyScore = -30 // 30 point bonus for 72-76
		} else if energyDiff <= 5 {
			startingEnergyScore = -10 // 10 point bonus for 69-79
		} else if energyDiff > 15 {
			startingEnergyScore = 20 // 20 point penalty for being far from target
		}
		// No penalty/bonus for 6-15 point difference
	}

	// Calculate length metrics
	mixLength := len(mix)
	utilizationRate := float64(mixLength) / float64(totalInputTracks)

	// Length reward/penalty system
	lengthScore := 0

	if mixLength < 10 {
		// Very short mixes get heavy penalty (even if perfect quality)
		lengthScore = (10 - mixLength) * 50 // 50 points per missing track below 10
	} else if mixLength < 20 {
		// Short mixes get moderate penalty
		lengthScore = (20 - mixLength) * 20 // 20 points per missing track below 20
	} else if mixLength < 30 {
		// Reasonable length mixes get small penalty
		lengthScore = (30 - mixLength) * 5 // 5 points per missing track below 30
	} else if mixLength <= 50 {
		// Good length mixes get no penalty
		lengthScore = 0
	} else {
		// Very long mixes get small penalty for being unwieldy
		lengthScore = (mixLength - 50) * 2
	}

	// Utilization bonus: reward using more of the available tracks
	utilizationBonus := 0
	if utilizationRate >= 0.4 { // Using 40%+ of tracks
		utilizationBonus = int(utilizationRate * 100) // Up to 100 point bonus
	}

	// Calculate average quality per track (to normalize for length)
	avgQualityPerTrack := float64(qualityScore) / float64(mixLength)

	// Quality threshold: penalize mixes with poor average quality
	qualityThresholdPenalty := 0
	if avgQualityPerTrack > 8.0 { // If average penalty > 8 per track
		qualityThresholdPenalty = int((avgQualityPerTrack - 8.0) * 100)
	}

	// Composite score: balance all factors including starting energy adherence
	compositeScore := qualityScore + lengthScore + qualityThresholdPenalty - utilizationBonus + startingEnergyScore

	return compositeScore
}

// generateSingleMix creates one candidate mix using the constance strategy
func (s *ConstanceSorter) generateSingleMix(tracks []track.Track, seed int64, targetLimit int) []track.Track {
	rng := rand.New(rand.NewSource(seed))

	// Create fresh bucket system for this candidate
	buckets := NewConstanceBuckets(tracks, rng)

	var ordered []track.Track
	var prevTrack *track.Track
	energyRampState := &EnergyRampState{
		isBuilding:       true,
		tracksSinceStart: 0,
		targetRampLength: 8 + rng.Intn(5), // 8-12 track ramps
	}

	// Track consecutive same-key runs to prevent excessive repetition
	sameKeyRunCount := 0
	maxSameKeyRun := 3 // Allow at most 3 consecutive same-key transitions

	for len(ordered) < targetLimit && buckets.GetTotalTracks() > 0 {
		var nextTrack track.Track
		var found bool

		if prevTrack == nil {
			// Step 1: Pick first song (highest energy to start strong)
			nextTrack, found = s.pickBestStartingTrack(buckets, energyRampState)
		} else {
			// Step 2: Check if we should stop to maintain quality
			if s.shouldStopForQuality(buckets, *prevTrack, sameKeyRunCount, maxSameKeyRun) {
				break // Stop here to maintain mix quality
			}

			// Step 3: Pick next song based on constance rules with buckets
			trackPosition := len(ordered) + 1 // Position of the track we're about to add
			nextTrack, found = s.pickNextTrackByRulesWithBuckets(buckets, *prevTrack, energyRampState, rng, sameKeyRunCount, maxSameKeyRun, trackPosition)
		}

		if !found {
			break // No more tracks available
		}

		// Track same-key runs
		if prevTrack != nil && nextTrack.Key.Number == prevTrack.Key.Number && nextTrack.Key.Mode == prevTrack.Key.Mode {
			sameKeyRunCount++
		} else {
			sameKeyRunCount = 0
		}

		// Add to ordered list
		ordered = append(ordered, nextTrack)
		prevTrack = &nextTrack
		energyRampState.tracksSinceStart++

		// Update energy ramp state if needed
		if energyRampState.tracksSinceStart >= energyRampState.targetRampLength {
			energyRampState.isBuilding = !energyRampState.isBuilding
			energyRampState.tracksSinceStart = 0
			energyRampState.targetRampLength = 6 + rng.Intn(7) // 6-12 track ramps
		}
	}

	return ordered
}

// EnergyRampState tracks our energy building/dropping cycles
type EnergyRampState struct {
	isBuilding       bool // true if we're building energy, false if dropping
	tracksSinceStart int  // tracks since last direction change
	targetRampLength int  // how many tracks in current ramp
}

// TransitionType represents the three types of transitions constance uses
type TransitionType int

const (
	SparkleJump TransitionType = iota // 10%: +1 num, switch letter (2A->3B)
	SameKey                           // 30%: same key (8A->8A)
	NumIncrease                       // 60%: +1 num, same letter (5B->6B, 12A->1A)
)

// pickBestStartingTrack selects the best track to start with from buckets
func (s *ConstanceSorter) pickBestStartingTrack(buckets *ConstanceBuckets, energyState *EnergyRampState) (track.Track, bool) {
	if buckets.GetTotalTracks() == 0 {
		return track.Track{}, false
	}

	var bestKey track.Key
	bestScore := -1.0
	found := false

	// Look through all buckets for the best starting track
	for key, bucket := range buckets.buckets {
		if len(bucket.Tracks) == 0 {
			continue
		}

		// Score each track in this bucket for starting potential
		for _, t := range bucket.Tracks {
			score := s.scoreStartingTrack(t, buckets)
			if score > bestScore {
				bestScore = score
				bestKey = key
				found = true
			}
		}
	}

	if !found {
		return track.Track{}, false
	}

	// Remove the best track from its bucket
	selectedTrack, _ := buckets.GetTrackFromBucket(bestKey, track.Track{}, energyState)
	return selectedTrack, true
}

// scoreStartingTrack gives a score for how good a starting track is
// Opinionated: targets energy 74 specifically for optimal build progression
func (s *ConstanceSorter) scoreStartingTrack(t track.Track, buckets *ConstanceBuckets) float64 {
	// Target energy: specifically 74 for optimal progression
	targetEnergy := 74.0
	trackEnergy := float64(t.Energy)

	// Energy scoring: heavily favor tracks close to 74
	energyDiff := math.Abs(trackEnergy - targetEnergy)
	energyScore := 0.0

	if energyDiff == 0 {
		energyScore = 1.0 // Perfect match at 74
	} else if energyDiff <= 2 {
		energyScore = 0.95 // Very close (72-76)
	} else if energyDiff <= 5 {
		energyScore = 0.8 // Close enough (69-79)
	} else if energyDiff <= 10 {
		energyScore = 0.5 // Acceptable range (64-84)
	} else {
		energyScore = 0.1 // Too far from target
	}

	// Reasonable BPM range (not too extreme)
	bpmScore := 1.0
	if t.BPM < 100 || t.BPM > 140 {
		bpmScore = 0.5 // Penalty for extreme BPMs
	}

	// Key compatibility - starting keys that have good transitions available
	keyCompatibilityScore := s.scoreStartingKeyCompatibility(t.Key, buckets)

	return energyScore*0.7 + bpmScore*0.2 + keyCompatibilityScore*0.1
}

// scoreStartingKeyCompatibility scores how good a key is for starting based on available transitions
func (s *ConstanceSorter) scoreStartingKeyCompatibility(key track.Key, buckets *ConstanceBuckets) float64 {
	// Count available transitions from this starting key
	transitionCount := s.countAvailableTransitions(buckets, key)

	// Normalize: 3+ good transitions = perfect score
	return math.Min(1.0, float64(transitionCount)/3.0)
}

// shouldStopForQuality determines if we should stop mixing to maintain quality
func (s *ConstanceSorter) shouldStopForQuality(buckets *ConstanceBuckets, prev track.Track, sameKeyRunCount, maxSameKeyRun int) bool {
	// If we're in a long same-key run, check if we can break out of it
	if sameKeyRunCount >= maxSameKeyRun {
		// Can we make any good transitions from current key?
		if !s.hasGoodTransitionsAvailable(buckets, prev.Key) {
			return true // Stop - we'd be forced into more bad same-key transitions
		}
	}

	// Check if we have enough variety left for quality mixing
	availableTransitions := s.countAvailableTransitions(buckets, prev.Key)
	if availableTransitions < 2 {
		// We're running low on good options - consider stopping
		totalRemaining := buckets.GetTotalTracks()
		if totalRemaining > 10 {
			// Lots of tracks left but poor transition options - stop for quality
			return true
		}
	}

	return false // Continue mixing
}

// hasGoodTransitionsAvailable checks if we can make quality transitions from a key
func (s *ConstanceSorter) hasGoodTransitionsAvailable(buckets *ConstanceBuckets, from track.Key) bool {
	// Check for NumIncrease transitions (60% preference)
	nextNum := from.Number + 1
	if nextNum > 12 {
		nextNum = 1
	}
	if buckets.GetBucketCount(track.Key{Number: nextNum, Mode: from.Mode}) > 0 {
		return true
	}

	// Check for SparkleJump transitions (10% preference)
	oppositeMode := track.ModeA
	if from.Mode == track.ModeA {
		oppositeMode = track.ModeB
	}
	if buckets.GetBucketCount(track.Key{Number: nextNum, Mode: oppositeMode}) > 0 {
		return true
	}

	return false // Only same-key transitions available
}

// countAvailableTransitions counts how many good transition options we have
func (s *ConstanceSorter) countAvailableTransitions(buckets *ConstanceBuckets, from track.Key) int {
	count := 0

	// Count NumIncrease option
	nextNum := from.Number + 1
	if nextNum > 12 {
		nextNum = 1
	}
	if buckets.GetBucketCount(track.Key{Number: nextNum, Mode: from.Mode}) > 0 {
		count++
	}

	// Count SparkleJump option
	oppositeMode := track.ModeA
	if from.Mode == track.ModeA {
		oppositeMode = track.ModeB
	}
	if buckets.GetBucketCount(track.Key{Number: nextNum, Mode: oppositeMode}) > 0 {
		count++
	}

	// Count SameKey option (but only if we haven't been using it too much)
	if buckets.GetBucketCount(from) > 0 {
		count++
	}

	return count
}

// pickNextTrackByRulesWithBuckets implements the constance transition rules using buckets
// Now energy-aware for early mix progression (no drops until track 4-5 or 5-6)
func (s *ConstanceSorter) pickNextTrackByRulesWithBuckets(buckets *ConstanceBuckets, prev track.Track, energyState *EnergyRampState, rng *rand.Rand, sameKeyRunCount, maxSameKeyRun, trackPosition int) (track.Track, bool) {
	if buckets.GetTotalTracks() == 0 {
		return track.Track{}, false
	}

	// Choose transition types, avoiding same-key if we're in a run
	var transitionTypes []TransitionType

	if sameKeyRunCount >= maxSameKeyRun {
		// Force a key change - no same-key transitions allowed
		transitionTypes = []TransitionType{NumIncrease, SparkleJump}
	} else {
		// Normal transition selection with all options
		primaryChoice := s.chooseTransitionType(rng)
		transitionTypes = []TransitionType{primaryChoice}

		// Add fallback transitions if primary doesn't work
		for _, fallback := range []TransitionType{NumIncrease, SameKey, SparkleJump} {
			if fallback != primaryChoice {
				transitionTypes = append(transitionTypes, fallback)
			}
		}
	}

	// Try each transition type until we find a track
	for _, transType := range transitionTypes {
		targetKeys := s.getTargetKeysForTransition(prev.Key, transType)

		for _, targetKey := range targetKeys {
			if buckets.GetBucketCount(targetKey) > 0 {
				track, found := s.getEnergyAwareTrackFromBucket(buckets, targetKey, prev, energyState, trackPosition)
				if found {
					return track, true
				}
			}
		}
	}

	// Last resort: pick any available track with energy-aware harmonic compatibility
	return s.pickBestAvailableTrackWithEnergyConstraints(buckets, prev, energyState, trackPosition)
}

// getEnergyAwareTrackFromBucket selects tracks considering energy progression constraints
func (s *ConstanceSorter) getEnergyAwareTrackFromBucket(buckets *ConstanceBuckets, targetKey track.Key, prev track.Track, energyState *EnergyRampState, trackPosition int) (track.Track, bool) {
	bucket, exists := buckets.buckets[targetKey]
	if !exists || len(bucket.Tracks) == 0 {
		return track.Track{}, false
	}

	// Define energy constraints based on track position
	minEnergyRequired := prev.Energy
	if trackPosition <= 4 {
		// Tracks 2-4: no energy drops allowed, prefer increases
		minEnergyRequired = prev.Energy
	} else if trackPosition == 5 {
		// Track 5: allow small drops but prefer maintains/increases
		minEnergyRequired = prev.Energy - 3
	} else {
		// Track 6+: normal energy ramp logic applies
		minEnergyRequired = prev.Energy - 15 // Allow normal drops
	}

	// Find candidate tracks that meet energy constraints
	var validCandidates []track.Track
	for _, t := range bucket.Tracks {
		if t.Energy >= minEnergyRequired {
			validCandidates = append(validCandidates, t)
		}
	}

	// If no valid candidates with energy constraints, relax constraints after track 4
	if len(validCandidates) == 0 && trackPosition > 4 {
		validCandidates = bucket.Tracks // Use all tracks as fallback
	}

	if len(validCandidates) == 0 {
		return track.Track{}, false
	}

	// Score and select the best candidate
	bestIdx := 0
	bestScore := s.scoreEnergyAwareCandidate(validCandidates[0], prev, energyState, trackPosition)

	for i := 1; i < len(validCandidates); i++ {
		score := s.scoreEnergyAwareCandidate(validCandidates[i], prev, energyState, trackPosition)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	// Remove the selected track from the original bucket
	selectedTrack := validCandidates[bestIdx]
	for i, t := range bucket.Tracks {
		if t.Title == selectedTrack.Title && t.Artist == selectedTrack.Artist {
			bucket.Tracks = append(bucket.Tracks[:i], bucket.Tracks[i+1:]...)
			break
		}
	}

	return selectedTrack, true
}

// scoreEnergyAwareCandidate scores tracks considering energy progression and track position
func (s *ConstanceSorter) scoreEnergyAwareCandidate(candidate, prev track.Track, energyState *EnergyRampState, trackPosition int) float64 {
	energyChange := candidate.Energy - prev.Energy

	// Energy scoring based on track position
	energyScore := 0.0
	if trackPosition <= 4 {
		// Early tracks: heavily favor energy increases or maintains
		if energyChange > 5 {
			energyScore = 1.0 // Strong energy build
		} else if energyChange >= 0 {
			energyScore = 0.9 // Maintain or small build
		} else if energyChange >= -3 {
			energyScore = 0.3 // Small drop (discouraged)
		} else {
			energyScore = 0.0 // Larger drop (heavily discouraged)
		}
	} else {
		// Later tracks: normal energy ramp logic
		if energyState.isBuilding {
			if energyChange > 0 {
				energyScore = 1.0
			} else if energyChange >= -5 {
				energyScore = 0.7
			} else {
				energyScore = 0.3
			}
		} else {
			if energyChange < -10 {
				energyScore = 1.0 // Good drop
			} else if energyChange < 0 {
				energyScore = 0.8
			} else {
				energyScore = 0.5
			}
		}
	}

	// BPM compatibility
	bpmDiff := math.Abs(candidate.BPM - prev.BPM)
	bpmScore := math.Max(0, 1.0-bpmDiff/20.0)

	return energyScore*0.7 + bpmScore*0.3
}

// pickBestAvailableTrackWithEnergyConstraints fallback selection with energy awareness
func (s *ConstanceSorter) pickBestAvailableTrackWithEnergyConstraints(buckets *ConstanceBuckets, prev track.Track, energyState *EnergyRampState, trackPosition int) (track.Track, bool) {
	var bestKey track.Key
	bestScore := -1000.0
	found := false

	for key, bucket := range buckets.buckets {
		if len(bucket.Tracks) == 0 {
			continue
		}

		harmonicScore := s.scoreHarmonicTransition(prev.Key, key)

		for _, t := range bucket.Tracks {
			energyScore := s.scoreEnergyAwareCandidate(t, prev, energyState, trackPosition)
			totalScore := harmonicScore*0.5 + energyScore*0.5

			if totalScore > bestScore {
				bestScore = totalScore
				bestKey = key
				found = true
			}
		}
	}

	if !found {
		return track.Track{}, false
	}

	return s.getEnergyAwareTrackFromBucket(buckets, bestKey, prev, energyState, trackPosition)
}

// chooseTransitionType randomly selects transition type based on probabilities
func (s *ConstanceSorter) chooseTransitionType(rng *rand.Rand) TransitionType {
	roll := rng.Float64()
	if roll < 0.10 {
		return SparkleJump // 10%
	} else if roll < 0.40 {
		return SameKey // 30%
	} else {
		return NumIncrease // 60%
	}
}

// getTargetKeysForTransition returns the keys that match a specific transition type
func (s *ConstanceSorter) getTargetKeysForTransition(from track.Key, transType TransitionType) []track.Key {
	switch transType {
	case SparkleJump:
		// +1 num, switch mode (2A->3B, 12B->1A)
		nextNum := from.Number + 1
		if nextNum > 12 {
			nextNum = 1
		}
		oppositeMode := track.ModeA
		if from.Mode == track.ModeA {
			oppositeMode = track.ModeB
		}
		return []track.Key{{Number: nextNum, Mode: oppositeMode}}

	case SameKey:
		// Exact same key (8A->8A)
		return []track.Key{from}

	case NumIncrease:
		// +1 num, same mode (5B->6B, 12A->1A)
		nextNum := from.Number + 1
		if nextNum > 12 {
			nextNum = 1
		}
		return []track.Key{{Number: nextNum, Mode: from.Mode}}

	default:
		return []track.Key{}
	}
}

// pickBestAvailableTrack picks the best track from any bucket when patterns fail
func (s *ConstanceSorter) pickBestAvailableTrack(buckets *ConstanceBuckets, prev track.Track, energyState *EnergyRampState) (track.Track, bool) {
	var bestKey track.Key
	bestScore := -1000.0 // Very low starting score
	found := false

	// Look through all buckets for harmonically compatible tracks
	for key, bucket := range buckets.buckets {
		if len(bucket.Tracks) == 0 {
			continue
		}

		// Score this key transition harmonically
		harmonicScore := s.scoreHarmonicTransition(prev.Key, key)

		// Find best track in this bucket
		for _, t := range bucket.Tracks {
			bpmScore := s.scoreBPMCompatibility(t.BPM, prev.BPM)
			energyScore := s.scoreEnergyCompatibility(t.Energy, prev.Energy, energyState)

			totalScore := harmonicScore*0.5 + bpmScore*0.3 + energyScore*0.2

			if totalScore > bestScore {
				bestScore = totalScore
				bestKey = key
				found = true
			}
		}
	}

	if !found {
		return track.Track{}, false
	}

	// Remove the best track from its bucket
	selectedTrack, _ := buckets.GetTrackFromBucket(bestKey, prev, energyState)
	return selectedTrack, true
}

// scoreHarmonicTransition scores how good a key transition is harmonically
func (s *ConstanceSorter) scoreHarmonicTransition(from, to track.Key) float64 {
	numDiff := to.Number - from.Number
	if numDiff < 0 {
		numDiff += 12 // Handle wrap-around
	}

	sameLetter := from.Mode == to.Mode

	// Perfect transitions
	if numDiff == 0 && sameLetter {
		return 1.0 // Same key
	}
	if numDiff == 1 && sameLetter {
		return 0.9 // +1 same mode
	}
	if numDiff == 1 && !sameLetter {
		return 0.8 // +1 different mode (sparkle jump)
	}
	if numDiff == 0 && !sameLetter {
		return 0.8 // Same number, different mode
	}

	// Good transitions
	if numDiff == 2 && sameLetter {
		return 0.6 // +2 same mode
	}
	if numDiff == 11 && sameLetter {
		return 0.7 // -1 same mode
	}

	// Everything else gets lower scores
	return math.Max(0.1, 0.8-float64(numDiff)*0.1)
}

// scoreBPMCompatibility scores BPM transitions
func (s *ConstanceSorter) scoreBPMCompatibility(candidateBPM, prevBPM float64) float64 {
	bpmDiff := math.Abs(candidateBPM - prevBPM)
	return math.Max(0, 1.0-bpmDiff/20.0)
}

// scoreEnergyCompatibility scores energy transitions based on ramp state
func (s *ConstanceSorter) scoreEnergyCompatibility(candidateEnergy, prevEnergy int, energyState *EnergyRampState) float64 {
	energyDiff := candidateEnergy - prevEnergy

	if energyState.isBuilding {
		// We want energy to increase (or at least not decrease much)
		if energyDiff > 0 {
			return 1.0 // Perfect - energy increasing
		} else if energyDiff >= -5 {
			return 0.7 // OK - small decrease
		} else {
			return 0.3 // Poor - large decrease when we want to build
		}
	} else {
		// We want energy to decrease for the drop
		if energyDiff < -10 {
			return 1.0 // Perfect - big energy drop
		} else if energyDiff < 0 {
			return 0.8 // Good - some energy drop
		} else if energyDiff <= 5 {
			return 0.5 // OK - stable energy
		} else {
			return 0.2 // Poor - energy increasing when we want a drop
		}
	}
}
