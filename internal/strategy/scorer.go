package strategy

import (
	"fmt"

	"github.com/YakDriver/magicmix/internal/track"
)

// MixScore represents the comprehensive scoring breakdown for a mix
type MixScore struct {
	Total       int          // Overall mix quality score (0 = perfect)
	KeyScore    KeyScore     // Key transition scoring
	BPMScore    *BPMScore    // BPM transition scoring (future)
	EnergyScore *EnergyScore // Energy flow scoring (future)
}

// KeyScore represents the scoring breakdown for key transitions only
type KeyScore struct {
	Total             int                   // Total key penalty points (0 = perfect)
	ModeAndNumberPts  int                   // A/B + number changes
	NumberChangePts   int                   // Number direction penalties
	RunPenaltyPts     int                   // Consecutive same key penalties
	TransitionDetails []KeyTransitionDetail // Detailed breakdown per transition
}

// KeyTransitionDetail captures the scoring for each individual key transition
type KeyTransitionDetail struct {
	FromKey          track.Key
	ToKey            track.Key
	ModeAndNumberPts int
	NumberChangePts  int
	RunPenaltyPts    int
	RunLength        int
	Description      string
}

// BPMScore represents the scoring breakdown for BPM transitions
type BPMScore struct {
	Total             int                   // Total BPM penalty points (0 = perfect)
	TransitionPts     int                   // BPM change penalties
	RangePenaltyPts   int                   // Cross-genre range penalties
	TempoShockPts     int                   // Extreme tempo change penalties
	TransitionDetails []BPMTransitionDetail // Detailed breakdown per transition
}

// BPMTransitionDetail captures the scoring for each individual BPM transition
type BPMTransitionDetail struct {
	FromBPM         float64
	ToBPM           float64
	Change          float64 // Absolute BPM change
	PercentChange   float64 // Percentage change
	TransitionPts   int     // Points for this transition
	RangePenaltyPts int     // Cross-genre penalty
	TempoShockPts   int     // Extreme change penalty
	Description     string  // Human-readable description
}

// EnergyScore represents the scoring breakdown for energy flow
type EnergyScore struct {
	Total             int                      // Total energy penalty points (0 = perfect)
	EnergyFlowPts     int                      // Energy flow continuity penalties
	KeyProgressionPts int                      // Key-based energy progression penalties
	EnergyDropPts     int                      // Harsh energy drop penalties
	PlateauPts        int                      // Energy plateau penalties
	TransitionDetails []EnergyTransitionDetail // Detailed breakdown per transition
}

// EnergyTransitionDetail captures the scoring for each individual energy transition
type EnergyTransitionDetail struct {
	FromEnergy        int
	ToEnergy          int
	FromKeyEnergy     float64 // Key-based energy component
	ToKeyEnergy       float64 // Key-based energy component
	EnergyChange      int     // Raw energy change
	KeyEnergyChange   float64 // Key progression energy change
	EnergyFlowPts     int     // Energy flow penalty
	KeyProgressionPts int     // Key progression penalty
	EnergyDropPts     int     // Energy drop penalty
	Description       string  // Human-readable description
}

// ScoreMix evaluates the overall quality of a track mix
// Combines key, BPM, and energy scoring for comprehensive DJ mix evaluation
func ScoreMix(tracks []track.Track) MixScore {
	keyScore := ScoreKeyTransitions(tracks)
	bpmScore := ScoreBPMTransitions(tracks)
	energyScore := ScoreEnergyFlow(tracks)

	return MixScore{
		Total:       keyScore.Total + bpmScore.Total + energyScore.Total,
		KeyScore:    keyScore,
		BPMScore:    &bpmScore,
		EnergyScore: &energyScore,
	}
}

// ScoreKeyTransitions evaluates the quality of key transitions in a track list
// Returns a score where 0 is perfect and higher numbers indicate more problems
func ScoreKeyTransitions(tracks []track.Track) KeyScore {
	if len(tracks) <= 1 {
		return KeyScore{}
	}

	details := make([]KeyTransitionDetail, 0, len(tracks)-1)
	totalModeAndNumber := 0
	totalNumberChange := 0
	totalRunPenalty := 0

	// Track runs of the same key (number + letter)
	currentRunKey := tracks[0].Key
	currentRunLength := 1

	for i := 1; i < len(tracks); i++ {
		prev := tracks[i-1]
		curr := tracks[i]

		detail := KeyTransitionDetail{
			FromKey: prev.Key,
			ToKey:   curr.Key,
		}

		// Update run tracking
		if curr.Key == currentRunKey {
			currentRunLength++
		} else {
			// Score the completed run
			if currentRunLength > 2 {
				runPts := scoreKeyRun(currentRunLength)
				// Apply run penalty to the last transition of the run
				if len(details) > 0 {
					details[len(details)-1].RunPenaltyPts += runPts
					details[len(details)-1].RunLength = currentRunLength
					totalRunPenalty += runPts
				}
			}
			currentRunKey = curr.Key
			currentRunLength = 1
		}

		// Score A/B related changes
		modeAndNumberPts := scoreModeAndNumberChange(prev.Key, curr.Key)
		detail.ModeAndNumberPts = modeAndNumberPts
		totalModeAndNumber += modeAndNumberPts

		// Score number changes with DJ-realistic logic
		numberChangePts := scoreKeyNumberChange(prev.Key, curr.Key)
		detail.NumberChangePts = numberChangePts
		totalNumberChange += numberChangePts

		// Build description
		detail.Description = buildKeyTransitionDescription(prev.Key, curr.Key, detail.RunLength)

		details = append(details, detail)
	}

	// Handle final run if needed
	if currentRunLength > 2 {
		runPts := scoreKeyRun(currentRunLength)
		if len(details) > 0 {
			details[len(details)-1].RunPenaltyPts += runPts
			details[len(details)-1].RunLength = currentRunLength
			totalRunPenalty += runPts
		}
	}

	return KeyScore{
		Total:             totalModeAndNumber + totalNumberChange + totalRunPenalty,
		ModeAndNumberPts:  totalModeAndNumber,
		NumberChangePts:   totalNumberChange,
		RunPenaltyPts:     totalRunPenalty,
		TransitionDetails: details,
	}
}

// scoreModeAndNumberChange penalizes transitions that change both A/B mode and key number
// In DJ mixing, changing both makes transitions much harder to execute smoothly
func scoreModeAndNumberChange(from, to track.Key) int {
	if from.Mode != to.Mode && from.Number != to.Number {
		return 5 // Challenging but manageable for changing both A/B and number
	}
	return 0
}

// scoreKeyNumberChange evaluates key number transitions using DJ mixing principles
// Camelot wheel physics: adjacent numbers (±1) are harmonically compatible,
// perfect matches (±0) are ideal, and larger jumps become increasingly difficult
func scoreKeyNumberChange(from, to track.Key) int {
	// Calculate the shortest distance around the wheel (1-12 wrapping)
	diff := to.Number - from.Number

	// Handle wrapping around the wheel
	if diff > 6 {
		diff = diff - 12 // e.g., 11→2 becomes -9→-3
	} else if diff < -6 {
		diff = diff + 12 // e.g., 2→11 becomes -9→3
	}

	// Apply DJ-realistic penalties based on harmonic compatibility
	switch diff {
	case 0:
		return 0 // Perfect match - same key number
	case 1, -1:
		return 0 // Adjacent keys - harmonically compatible (basic mixing)
	case 2, -2:
		return 1 // Two steps - acceptable but requires skill
	case 3, -3:
		return 3 // Three steps - manageable with technique, still mixable
	case 4, -4:
		return 6 // Four steps - difficult but achievable
	case 5, -5:
		return 8 // Five steps - very challenging, noticeable dissonance
	case 6, -6:
		return 10 // Tritone - harmonically catastrophic, should be avoided
	default:
		return 10 // Safety net for any edge cases
	}
}

// scoreKeyRun penalizes consecutive tracks with the same key
// In DJ mixing, too many tracks in the same key creates monotony
func scoreKeyRun(runLength int) int {
	if runLength <= 2 {
		return 0 // 2 tracks in same key is acceptable
	}
	// Exponential penalty growth for longer runs
	// 3 tracks = 2pts, 4 tracks = 6pts, 5 tracks = 12pts, etc.
	return (runLength - 2) * (runLength - 1)
}

// buildKeyTransitionDescription creates a human-readable description of the transition
func buildKeyTransitionDescription(from, to track.Key, runLength int) string {
	if from == to {
		if runLength > 2 {
			return "Same key (long run)"
		}
		return "Same key"
	}

	// Calculate key number change for description
	diff := to.Number - from.Number
	if diff > 6 {
		diff = diff - 12
	} else if diff < -6 {
		diff = diff + 12
	}

	// Describe the transition quality
	absNumberDiff := diff
	if absNumberDiff < 0 {
		absNumberDiff = -absNumberDiff
	}

	modeChange := from.Mode != to.Mode

	switch {
	case absNumberDiff == 0 && !modeChange:
		return "Perfect (same key)"
	case absNumberDiff <= 1 && !modeChange:
		return "Excellent (adjacent, same mode)"
	case absNumberDiff <= 1 && modeChange:
		return "Good (adjacent, mode change)"
	case absNumberDiff <= 2 && !modeChange:
		return "Acceptable (2 steps, same mode)"
	case absNumberDiff <= 2 && modeChange:
		return "Challenging (2 steps + mode change)"
	case absNumberDiff <= 3:
		return "Manageable (3 steps, requires technique)"
	case absNumberDiff <= 4:
		return "Difficult (4 steps)"
	case absNumberDiff <= 5:
		return "Very difficult (5 steps)"
	default:
		return "Harmonically catastrophic (tritone/6 steps)"
	}
}

// ScoreBPMTransitions evaluates the quality of BPM transitions in a track list
// Returns a score where 0 is perfect and higher numbers indicate more problems
// Based on DJ mixing principles: gradual changes are preferred, extreme jumps are problematic
func ScoreBPMTransitions(tracks []track.Track) BPMScore {
	if len(tracks) <= 1 {
		return BPMScore{}
	}

	details := make([]BPMTransitionDetail, 0, len(tracks)-1)
	totalTransition := 0
	totalRangePenalty := 0
	totalTempoShock := 0

	for i := 1; i < len(tracks); i++ {
		prev := tracks[i-1]
		curr := tracks[i]

		detail := BPMTransitionDetail{
			FromBPM: prev.BPM,
			ToBPM:   curr.BPM,
		}

		// Calculate BPM change metrics
		change := curr.BPM - prev.BPM
		if change < 0 {
			change = -change
		}
		detail.Change = change
		detail.PercentChange = (change / prev.BPM) * 100

		// Score BPM transition based on DJ mixing principles
		transitionPts := scoreBPMChange(change, detail.PercentChange)
		detail.TransitionPts = transitionPts
		totalTransition += transitionPts

		// Score cross-genre range penalties (different BPM categories)
		rangePenalty := scoreBPMRangePenalty(prev.BPM, curr.BPM)
		detail.RangePenaltyPts = rangePenalty
		totalRangePenalty += rangePenalty

		// Score extreme tempo shocks
		tempoShock := scoreBPMTempoShock(detail.PercentChange)
		detail.TempoShockPts = tempoShock
		totalTempoShock += tempoShock

		// Build description
		detail.Description = buildBPMTransitionDescription(detail)

		details = append(details, detail)
	}

	return BPMScore{
		Total:             totalTransition + totalRangePenalty + totalTempoShock,
		TransitionPts:     totalTransition,
		RangePenaltyPts:   totalRangePenalty,
		TempoShockPts:     totalTempoShock,
		TransitionDetails: details,
	}
}

// scoreBPMChange evaluates BPM transitions using DJ mixing principles
// Gradual changes are preferred, larger jumps become increasingly problematic
func scoreBPMChange(change, percentChange float64) int {
	// Penalty based on absolute BPM change - DJs can handle more than expected
	switch {
	case change <= 3:
		return 0 // Perfect - virtually unnoticeable
	case change <= 8:
		return 1 // Excellent - very smooth transition
	case change <= 15:
		return 2 // Good - manageable with skill
	case change <= 25:
		return 4 // Challenging but doable
	case change <= 35:
		return 7 // Difficult - requires advanced technique
	case change <= 50:
		return 12 // Very difficult - major tempo jump
	default:
		return 18 // Extreme - tempo shock, nearly impossible to mix smoothly
	}
}

// scoreBPMRangePenalty penalizes transitions between different musical genres/styles
// Based on typical BPM ranges for different dance music genres
func scoreBPMRangePenalty(fromBPM, toBPM float64) int {
	genre1 := getBPMGenreCategory(fromBPM)
	genre2 := getBPMGenreCategory(toBPM)

	if genre1 != genre2 {
		// Reduce penalty - many genres blend well
		// Only penalize extreme genre jumps
		if (genre1 == "downtempo" && (genre2 == "dance" || genre2 == "hardstyle")) ||
			(genre1 == "hardcore" && (genre2 == "downtempo" || genre2 == "midtempo")) {
			return 2 // Major genre mismatch
		}
		return 1 // Minor genre transition penalty
	}
	return 0
} // getBPMGenreCategory classifies BPM into typical dance music genre ranges
func getBPMGenreCategory(bpm float64) string {
	switch {
	case bpm < 90:
		return "downtempo" // Hip-hop, trap, chill
	case bpm < 110:
		return "midtempo" // Pop, R&B, some hip-hop
	case bpm < 130:
		return "uptempo" // House, commercial dance
	case bpm < 150:
		return "dance" // EDM, progressive house
	case bpm < 180:
		return "hardstyle" // Hard dance, drum & bass
	default:
		return "hardcore" // Hardcore, speedcore
	}
}

// scoreBPMTempoShock penalizes extreme percentage changes that shock the dancefloor
func scoreBPMTempoShock(percentChange float64) int {
	switch {
	case percentChange <= 8:
		return 0 // Smooth
	case percentChange <= 15:
		return 1 // Noticeable
	case percentChange <= 30:
		return 2 // Significant but manageable
	case percentChange <= 50:
		return 4 // Jarring
	default:
		return 6 // Major tempo shock
	}
}

// buildBPMTransitionDescription creates a human-readable description
func buildBPMTransitionDescription(detail BPMTransitionDetail) string {
	change := detail.Change
	percent := detail.PercentChange

	direction := "increase"
	if detail.ToBPM < detail.FromBPM {
		direction = "decrease"
	}

	switch {
	case change <= 2:
		return "Seamless tempo match"
	case change <= 5:
		return fmt.Sprintf("Smooth %s (%.1f BPM)", direction, change)
	case change <= 10:
		return fmt.Sprintf("Good %s (%.1f BPM)", direction, change)
	case change <= 15:
		return fmt.Sprintf("Noticeable %s (%.1f BPM)", direction, change)
	case change <= 20:
		return fmt.Sprintf("Difficult %s (%.1f BPM)", direction, change)
	case change <= 30:
		return fmt.Sprintf("Harsh %s (%.1f BPM, %.1f%%)", direction, change, percent)
	default:
		return fmt.Sprintf("Tempo shock (%.1f BPM, %.1f%%)", change, percent)
	}
}

// ScoreEnergyFlow evaluates the energy flow and progression in a track list
// Combines raw energy ratings with key-based energy progression theory
func ScoreEnergyFlow(tracks []track.Track) EnergyScore {
	if len(tracks) <= 1 {
		return EnergyScore{}
	}

	details := make([]EnergyTransitionDetail, 0, len(tracks)-1)
	totalEnergyFlow := 0
	totalKeyProgression := 0
	totalEnergyDrop := 0
	totalPlateau := 0

	// Track energy progression for plateau detection
	plateauCount := 0

	for i := 1; i < len(tracks); i++ {
		prev := tracks[i-1]
		curr := tracks[i]
		trackPosition := i // Position in mix (1-based)
		totalTracks := len(tracks)
		mixProgress := float64(trackPosition) / float64(totalTracks) // 0.0 to 1.0

		detail := EnergyTransitionDetail{
			FromEnergy:      prev.Energy,
			ToEnergy:        curr.Energy,
			FromKeyEnergy:   calculateKeyEnergy(prev.Key),
			ToKeyEnergy:     calculateKeyEnergy(curr.Key),
			EnergyChange:    curr.Energy - prev.Energy,
			KeyEnergyChange: calculateKeyEnergy(curr.Key) - calculateKeyEnergy(prev.Key),
		}

		// Score energy flow continuity
		energyFlowPts := scoreEnergyFlow(detail.EnergyChange, detail.KeyEnergyChange)
		detail.EnergyFlowPts = energyFlowPts
		totalEnergyFlow += energyFlowPts

		// Score key-based energy progression
		keyProgressionPts := scoreKeyEnergyProgression(detail.KeyEnergyChange, float64(detail.EnergyChange))
		detail.KeyProgressionPts = keyProgressionPts
		totalKeyProgression += keyProgressionPts

		// Score harsh energy drops with context awareness
		energyDropPts := scoreEnergyDropWithContext(detail.EnergyChange, trackPosition, mixProgress)
		detail.EnergyDropPts = energyDropPts
		totalEnergyDrop += energyDropPts

		// Track plateau progression
		if detail.EnergyChange >= -2 && detail.EnergyChange <= 2 {
			plateauCount++
		} else {
			plateauCount = 0
		}

		// Build description
		detail.Description = buildEnergyTransitionDescription(detail)

		details = append(details, detail)
	}

	// Score energy plateaus (too many flat sections)
	totalPlateau = scorePlateaus(plateauCount)

	return EnergyScore{
		Total:             totalEnergyFlow + totalKeyProgression + totalEnergyDrop + totalPlateau,
		EnergyFlowPts:     totalEnergyFlow,
		KeyProgressionPts: totalKeyProgression,
		EnergyDropPts:     totalEnergyDrop,
		PlateauPts:        totalPlateau,
		TransitionDetails: details,
	}
}

// calculateKeyEnergy assigns energy values based on key position in the Camelot wheel
// Higher numbers (approaching 12) have more energy, with B-side (major) having slightly more than A-side (minor)
// This reflects your insight about energy building: 5B → 7B → 9B → 11B → 12B → 2B
func calculateKeyEnergy(key track.Key) float64 {
	// Base energy from key number (1-12 scale)
	baseEnergy := float64(key.Number)

	// Adjust for the energy peak around 12 and valley around 6
	// This creates the natural progression you described
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

// scoreEnergyFlow evaluates energy progression using DJ principles
// Good energy flow gradually builds or maintains, with occasional strategic drops
func scoreEnergyFlow(energyChange int, keyEnergyChange float64) int {
	// Combine raw energy and key energy for holistic evaluation
	totalEnergyChange := float64(energyChange) + (keyEnergyChange * 0.3) // Weight key energy at 30%

	switch {
	case totalEnergyChange >= 3:
		return 0 // Good energy building
	case totalEnergyChange >= -2:
		return 0 // Acceptable energy maintenance or slight drop
	case totalEnergyChange >= -8:
		return 1 // Strategic energy drop - often necessary
	case totalEnergyChange >= -15:
		return 3 // Noticeable energy drop
	default:
		return 6 // Major energy crash
	}
} // scoreKeyEnergyProgression rewards progression that follows key energy theory
// Building through the wheel (5B → 7B → 9B → 11B → 12B → 2B) should be rewarded
func scoreKeyEnergyProgression(keyEnergyChange, rawEnergyChange float64) int {
	// If key energy and raw energy move in the same direction, that's good
	if (keyEnergyChange > 0 && rawEnergyChange > 0) || (keyEnergyChange < 0 && rawEnergyChange < 0) {
		return 0 // Key progression supports energy progression
	}

	// If they move in opposite directions, penalize based on magnitude
	energyConflict := keyEnergyChange - float64(rawEnergyChange)
	if energyConflict < 0 {
		energyConflict = -energyConflict
	}

	if energyConflict > 5 {
		return 3 // Significant conflict between key energy and track energy
	} else if energyConflict > 2 {
		return 1 // Minor conflict
	}
	return 0
}

// scoreEnergyDropWithContext penalizes energy drops based on magnitude, timing, and mix context
// Early drops are heavily penalized, mid-mix drops are more acceptable for energy ramps
func scoreEnergyDropWithContext(energyChange, trackPosition int, mixProgress float64) int {
	// If it's not really a drop, no penalty
	if energyChange >= -5 {
		return 0 // Tiny change or increase - perfectly fine
	}

	// Base penalty from magnitude (updated thresholds)
	basePenalty := 0
	if energyChange >= -12 {
		basePenalty = 0 // Small drop (like 86→75) - normal DJ technique
	} else if energyChange >= -20 {
		basePenalty = 2 // Moderate drop - noticeable but manageable
	} else if energyChange >= -30 {
		basePenalty = 5 // Significant drop
	} else if energyChange >= -40 {
		basePenalty = 10 // Major drop
	} else {
		basePenalty = 17 // Catastrophic drop (43+ points)
	}

	// Context multipliers based on track position
	contextMultiplier := 1.0

	if trackPosition <= 2 {
		// Very early in mix - drops are catastrophic (tracks 1-2)
		contextMultiplier = 3.0
	} else if trackPosition <= 5 {
		// Early in mix - drops are very bad (tracks 3-5)
		contextMultiplier = 2.0
	} else if mixProgress >= 0.8 {
		// Near end of mix - drops are more acceptable for wind-down
		contextMultiplier = 0.5
	} else {
		// Mid-mix - drops are part of energy ramp cycles
		// Estimate if we're in a ramp cycle (every 6-12 tracks)
		rampCyclePosition := trackPosition % 9 // Approximate 9-track cycles
		if rampCyclePosition <= 2 {
			// Early in ramp cycle - drops are acceptable for reset
			contextMultiplier = 0.3
		} else if rampCyclePosition >= 7 {
			// End of ramp cycle - drops are expected
			contextMultiplier = 0.2
		}
		// Otherwise use base multiplier (1.0) for mid-ramp
	}

	finalPenalty := max(
		// Ensure we don't go negative
		int(float64(basePenalty)*contextMultiplier), 0)

	return finalPenalty
}

// scorePlateaus penalizes long periods of flat energy
func scorePlateaus(plateauLength int) int {
	if plateauLength <= 4 {
		return 0 // Longer plateaus are acceptable for certain styles
	}
	return (plateauLength - 4) * 1 // Gentler penalty for extended plateaus
}

// buildEnergyTransitionDescription creates a human-readable description with realistic thresholds
func buildEnergyTransitionDescription(detail EnergyTransitionDetail) string {
	energyChange := detail.EnergyChange
	keyChange := detail.KeyEnergyChange

	energyDirection := "maintains"
	if energyChange > 2 {
		energyDirection = "builds"
	} else if energyChange < -2 {
		energyDirection = "drops"
	}

	keyDirection := ""
	if keyChange > 1 {
		keyDirection = " (key energy up)"
	} else if keyChange < -1 {
		keyDirection = " (key energy down)"
	}

	switch {
	case energyChange >= 10:
		return fmt.Sprintf("Strong energy build (+%d)%s", energyChange, keyDirection)
	case energyChange >= 5:
		return fmt.Sprintf("Good energy build (+%d)%s", energyChange, keyDirection)
	case energyChange >= -5:
		return fmt.Sprintf("Energy %s (%+d)%s", energyDirection, energyChange, keyDirection)
	case energyChange >= -12:
		return fmt.Sprintf("Strategic energy drop (%d)%s", energyChange, keyDirection)
	case energyChange >= -25:
		return fmt.Sprintf("Significant energy drop (%d)%s", energyChange, keyDirection)
	case energyChange >= -40:
		return fmt.Sprintf("Major energy drop (%d)%s", energyChange, keyDirection)
	default:
		return fmt.Sprintf("Catastrophic energy drop (%d)%s", energyChange, keyDirection)
	}
}
