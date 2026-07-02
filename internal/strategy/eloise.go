package strategy

import (
	"context"
	"math/rand"
	"time"

	"github.com/YakDriver/magicmix/internal/track"
)

const (
	eloiseStrategyName = "eloise"
)

// EloiseSorter implements a distribution-aware mixing strategy that prioritizes
// proper key burn rates alongside harmonic mixing principles
type EloiseSorter struct{}

func NewEloiseSorter() *EloiseSorter {
	return &EloiseSorter{}
}

func (s *EloiseSorter) Name() string {
	return eloiseStrategyName
}

func (s *EloiseSorter) Sort(ctx context.Context, tracks []track.Track) ([]track.Track, error) {
	if len(tracks) <= 1 {
		copied := make([]track.Track, len(tracks))
		for i, t := range tracks {
			copied[i] = t.Clone()
		}
		return copied, nil
	}

	// Initialize random number generator
	seed, ok := seedFromContext(ctx)
	if !ok || seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	// Get target limit from context (if any)
	targetLimit := limitFromContext(ctx)
	if targetLimit <= 0 {
		targetLimit = len(tracks) // Use full list if no limit specified
	}

	// Create working copy of tracks
	remaining := make([]track.Track, len(tracks))
	for i, t := range tracks {
		remaining[i] = t.Clone()
	}

	var ordered []track.Track
	var prevTrack *track.Track

	for len(ordered) < targetLimit && len(remaining) > 0 {
		// Step 1: Calculate limit-aware key distribution
		keyDistribution := calculateLimitAwareKeyDistribution(remaining, targetLimit, len(ordered))

		var nextTrack track.Track
		if prevTrack == nil {
			// Step 2: Randomly pick first song
			nextTrack = pickRandomFirst(remaining, rng)
		} else {
			// Step 4: Pick next song based on harmonization + limit-aware distribution
			nextTrack = pickNextTrack(remaining, *prevTrack, keyDistribution, rng)
		}

		// Add to ordered list and remove from remaining
		ordered = append(ordered, nextTrack)
		remaining = removeTrack(remaining, nextTrack)
		prevTrack = &nextTrack
	}

	return ordered, nil
}

// KeyDistribution holds the count and ratio information for each key number
type KeyDistribution struct {
	Total      int
	CountByKey map[int]int     // key number -> count
	RatioByKey map[int]float64 // key number -> ratio of total
	BurnRate   map[int]float64 // key number -> ideal tracks between uses
}

// calculateLimitAwareKeyDistribution analyzes the remaining tracks with target limit awareness
// This correctly handles scenarios where we want 20 tracks from 158 available tracks
func calculateLimitAwareKeyDistribution(tracks []track.Track, targetLimit, alreadySelected int) KeyDistribution {
	remainingSlots := targetLimit - alreadySelected
	if remainingSlots <= 0 {
		remainingSlots = 1 // At least 1 slot to prevent division by zero
	}

	countByKey := make(map[int]int)
	ratioByKey := make(map[int]float64)
	burnRate := make(map[int]float64)

	// Count tracks by key number
	for _, t := range tracks {
		countByKey[t.Key.Number]++
	}

	// Calculate ratios and burn rates based on TARGET LIMIT, not remaining tracks
	for keyNum, count := range countByKey {
		// Key insight: ratio should be based on how much we need in the final mix
		// Example: 29 tracks of 10A/10B available, but we only want 20 total tracks
		// So effective ratio = how much of this key should be in final 20-track mix

		// For limit-aware selection, we want proportional representation but capped
		// If we have 29 tracks of key 10 but only want 20 total tracks,
		// we might want at most 3-4 tracks of key 10 (not 29)
		expectedInFinal := float64(count) * float64(remainingSlots) / float64(len(tracks))

		// But don't let any single key dominate - cap at reasonable portion
		maxForKey := float64(remainingSlots) * 0.3 // Max 30% of remaining slots for any key
		if expectedInFinal > maxForKey {
			expectedInFinal = maxForKey
		}

		ratio := expectedInFinal / float64(remainingSlots)
		ratioByKey[keyNum] = ratio

		// Burn rate: how fast we should consume this key
		if expectedInFinal > 0 {
			burnRate[keyNum] = float64(count) / expectedInFinal
		} else {
			burnRate[keyNum] = float64(count) // Avoid this key
		}
	}

	return KeyDistribution{
		Total:      remainingSlots,
		CountByKey: countByKey,
		RatioByKey: ratioByKey,
		BurnRate:   burnRate,
	}
}

// calculateKeyDistribution analyzes the current remaining tracks (legacy function for backward compatibility)
func calculateKeyDistribution(tracks []track.Track) KeyDistribution {
	total := len(tracks)
	countByKey := make(map[int]int)
	ratioByKey := make(map[int]float64)
	burnRate := make(map[int]float64)

	// Count tracks by key number
	for _, t := range tracks {
		countByKey[t.Key.Number]++
	}

	// Calculate ratios and burn rates
	for keyNum, count := range countByKey {
		ratio := float64(count) / float64(total)
		ratioByKey[keyNum] = ratio

		// Burn rate = how many tracks should ideally pass between uses
		// High frequency keys (like 10A/10B with 29 tracks) should burn every ~5 tracks
		// Low frequency keys (like 5A/5B with 4 tracks) should burn every ~40 tracks
		if count > 0 {
			burnRate[keyNum] = float64(total) / float64(count)
		}
	}

	return KeyDistribution{
		Total:      total,
		CountByKey: countByKey,
		RatioByKey: ratioByKey,
		BurnRate:   burnRate,
	}
}

// pickRandomFirst selects the first track randomly (no bias needed)
func pickRandomFirst(tracks []track.Track, rng *rand.Rand) track.Track {
	return tracks[rng.Intn(len(tracks))]
}

// pickNextTrack selects the next track based on harmonic compatibility and distribution
func pickNextTrack(tracks []track.Track, prev track.Track, dist KeyDistribution, rng *rand.Rand) track.Track {
	// Score each candidate track
	bestScore := float64(-1000000)
	var candidates []track.Track

	for _, candidate := range tracks {
		score := scoreCandidate(candidate, prev, dist)

		if score > bestScore {
			bestScore = score
			candidates = []track.Track{candidate}
		} else if score == bestScore {
			candidates = append(candidates, candidate)
		}
	}

	// If multiple candidates have the same score, pick randomly
	if len(candidates) == 0 {
		return tracks[rng.Intn(len(tracks))]
	}
	return candidates[rng.Intn(len(candidates))]
}

// scoreCandidate evaluates how good a candidate track is for the next position
func scoreCandidate(candidate track.Track, prev track.Track, dist KeyDistribution) float64 {
	total := 0.0

	// Factor 1: Harmonic compatibility (DJ mixing principles) - OPTIMAL BALANCE
	harmonicScore := scoreHarmonicCompatibility(prev.Key, candidate.Key)
	total += harmonicScore * 150.0 // Optimal weight for harmonic excellence

	// Factor 2: BPM compatibility (tempo transition smoothness) - OPTIMIZED
	bpmScore := scoreBPMCompatibility(prev.BPM, candidate.BPM)
	total += bpmScore * 70.0 // Optimal weight for BPM flow

	// Factor 3: Energy progression (flow and key-based energy) - OPTIMIZED
	energyScore := scoreEnergyProgression(prev, candidate)
	total += energyScore * 45.0 // Optimized weight for energy flow

	// Factor 4: Distribution-based scoring (burn rate management) - INCREASED VARIETY
	distributionScore := scoreDistribution(candidate.Key.Number, dist)
	total += distributionScore * 15.0 // Increased weight for better key variety

	// Factor 5: Same key penalty (avoid consecutive identical keys)
	if prev.Key == candidate.Key {
		total -= 200.0 // Strong penalty for same key
	}

	return total
}

// scoreHarmonicCompatibility scores based on Camelot wheel harmonic relationships
func scoreHarmonicCompatibility(fromKey, toKey track.Key) float64 {
	// Calculate the shortest distance around the wheel (1-12 wrapping)
	diff := toKey.Number - fromKey.Number
	if diff > 6 {
		diff = diff - 12
	} else if diff < -6 {
		diff = diff + 12
	}

	// Apply harmonic compatibility scoring (EXTREME: ultra permissive)
	var numberScore float64
	switch {
	case diff == 0:
		numberScore = 1.0 // Perfect match
	case diff == 1 || diff == -1:
		numberScore = 1.0 // Adjacent keys - excellent for mixing
	case diff == 2 || diff == -2:
		numberScore = 0.9 // Two steps - excellent (improved from 0.8)
	case diff == 3 || diff == -3:
		numberScore = 0.8 // Three steps - good (improved from 0.6)
	case diff == 4 || diff == -4:
		numberScore = 0.6 // Four steps - acceptable (improved from 0.3)
	case diff == 5 || diff == -5:
		numberScore = 0.4 // Five steps - neutral+ (improved from 0.0)
	case diff == 6 || diff == -6:
		numberScore = -0.2 // Tritone - only mildly poor (improved from -0.5)
	}

	// Mode change penalty (EXTREME: ultra lenient)
	modeScore := 1.0
	if fromKey.Mode != toKey.Mode {
		if diff == 0 {
			modeScore = 0.98 // Same number, different mode - nearly perfect
		} else if diff == 1 || diff == -1 {
			modeScore = 0.95 // Adjacent with mode change - excellent
		} else if diff == 2 || diff == -2 {
			modeScore = 0.9 // Two steps with mode change - excellent
		} else {
			modeScore = 0.85 // Other combinations with mode change - good
		}
	}

	return numberScore * modeScore
}

// scoreDistribution scores based on how this key fits the limit-aware distribution needs
func scoreDistribution(keyNumber int, dist KeyDistribution) float64 {
	ratio := dist.RatioByKey[keyNumber]

	// For limited selections, be more aggressive about variety
	// If we only have 20 slots but 29 tracks of 10A/10B, we need to be very selective

	// Over-represented keys (ratio > ideal) get increasing burn pressure
	if ratio > 0.15 { // Very high frequency (>15% of target mix)
		return 3.0 // Strong pressure to use over-represented keys
	} else if ratio > 0.10 { // High frequency (10-15% of target mix)
		return 2.0 // Moderate pressure for high frequency keys
	} else if ratio > 0.05 { // Normal frequency (5-10% of target mix)
		return 1.0 // Slight bonus for normal representation
	} else if ratio > 0.02 { // Low frequency (2-5% of target mix)
		return 0.2 // Slight bonus for having variety
	} else if ratio > 0 { // Very rare keys (<2% of target mix)
		return -0.5 // Mild conservation - these add valuable variety
	} else {
		return -5.0 // Strong penalty for keys we don't have
	}
}

// removeTrack removes the first occurrence of a track from the slice
func removeTrack(tracks []track.Track, target track.Track) []track.Track {
	for i, t := range tracks {
		if tracksEqual(t, target) {
			// Remove by copying everything except this element
			result := make([]track.Track, 0, len(tracks)-1)
			result = append(result, tracks[:i]...)
			result = append(result, tracks[i+1:]...)
			return result
		}
	}
	return tracks // Track not found, return unchanged
}

// tracksEqual compares two tracks for equality
func tracksEqual(a, b track.Track) bool {
	return a.Title == b.Title && a.Artist == b.Artist && a.Key == b.Key && a.BPM == b.BPM && a.Energy == b.Energy
}

// scoreBPMCompatibility evaluates how well two BPMs transition together
// Returns a score from -1.0 (terrible) to 1.0 (perfect) based on DJ mixing principles
func scoreBPMCompatibility(fromBPM, toBPM float64) float64 {
	// Calculate BPM change
	change := toBPM - fromBPM
	if change < 0 {
		change = -change
	}

	// Score based on DJ mixing feasibility
	switch {
	case change <= 2:
		return 1.0 // Perfect - virtually unnoticeable
	case change <= 5:
		return 0.9 // Excellent - very smooth transition
	case change <= 10:
		return 0.7 // Good - noticeable but smooth
	case change <= 15:
		return 0.4 // Acceptable - requires skill but doable
	case change <= 20:
		return 0.1 // Difficult - noticeable jump
	case change <= 30:
		return -0.3 // Very difficult - obvious tempo change
	default:
		return -0.7 // Harsh - tempo shock
	}
}

// scoreEnergyProgression evaluates energy flow using both raw energy and key-based energy theory
// Returns a score from -1.0 (terrible flow) to 1.0 (perfect flow)
func scoreEnergyProgression(prev, candidate track.Track) float64 {
	// Calculate raw energy change
	energyChange := candidate.Energy - prev.Energy

	// Calculate key-based energy change using the wheel theory
	fromKeyEnergy := eloiseCalculateKeyEnergy(prev.Key)
	toKeyEnergy := eloiseCalculateKeyEnergy(candidate.Key)
	keyEnergyChange := toKeyEnergy - fromKeyEnergy

	// Combine raw energy and key energy for holistic evaluation
	// Weight key energy at 30% to emphasize the progression theory
	totalEnergyChange := float64(energyChange) + (keyEnergyChange * 0.3)

	// Score energy progression using DJ principles
	switch {
	case totalEnergyChange >= 5:
		return 1.0 // Excellent energy building
	case totalEnergyChange >= 2:
		return 0.8 // Good energy building
	case totalEnergyChange >= -2:
		return 0.6 // Acceptable energy maintenance
	case totalEnergyChange >= -5:
		return 0.3 // Slight energy drop - occasionally acceptable
	case totalEnergyChange >= -10:
		return -0.2 // Noticeable energy drop
	default:
		return -0.6 // Major energy crash - bad for dancefloor
	}
}

// eloiseCalculateKeyEnergy assigns energy values based on key position in the Camelot wheel
// Implements the energy progression theory: 5B → 7B → 9B → 11B → 12B → 2B
func eloiseCalculateKeyEnergy(key track.Key) float64 {
	// Base energy from key number (1-12 scale)
	baseEnergy := float64(key.Number)

	// Adjust for the energy peak around 12 and valley around 6
	// This creates the natural progression described
	if key.Number >= 10 {
		baseEnergy += 2.0 // Extra energy for peak keys (10, 11, 12)
	} else if key.Number <= 3 {
		baseEnergy += 1.0 // High energy for 1, 2, 3 (continuation from 12)
	} else if key.Number >= 5 && key.Number <= 7 {
		baseEnergy -= 1.0 // Lower energy in the middle range
	}

	// B-side (major) has slightly more energy than A-side (minor)
	if key.Mode == track.ModeB {
		baseEnergy += 0.5
	}

	return baseEnergy
}
