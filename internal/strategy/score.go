package strategy

import (
	"math"

	"github.com/YakDriver/magicmix/internal/track"
)

// This file is the single authoritative scoring model for an ordering. Both the
// --score report (ScoreMix) and the flow optimizer minimize the exact same value,
// so we always optimize what we measure. Lower is better; 0 is ideal.
//
// The score has two families:
//
//   - Coherence (pairwise): does each song feel related to its neighbor? Harmonic
//     Camelot compatibility and octave-folded tempo are always active; valence (mood)
//     and acousticness continuity are added when the data provides them.
//
//   - Contour (global): does the whole set have a satisfying energy shape? Intensity
//     (energy, blended with danceability when present) should move in waves —
//     sustained gradual builds punctuated by resets. A reset (a deliberate drop that
//     launches a new build) is free; the model instead penalizes jitter (choppy,
//     purposeless oscillation) and monotony (one endless ramp or a flat set). The
//     ending is intentionally neutral: "finish strong" depends on singalong/communal
//     payoff the data cannot see, so the engine does not pretend to know it.
//
// The model is adaptive: signals absent from the data simply contribute nothing, so
// a key/bpm/energy-only file is scored on coherence(harmonic+tempo)+contour, while a
// richer file additionally uses valence, danceability, and acousticness.

// Weights control the relative influence of each scoring family.
type Weights struct {
	Harmonic float64
	Tempo    float64
	Valence  float64
	Acoustic float64
	Contour  float64
}

// DefaultWeights is the shared configuration used by both ScoreMix and flow.
var DefaultWeights = Weights{
	Harmonic: 1.0,
	Tempo:    1.0,
	Valence:  0.5,
	Acoustic: 0.25,
	Contour:  1.0,
}

// Contour tuning constants. Intensity steps are on a 0-100 scale.
const (
	contourResetDrop  = 12.0 // an intensity drop larger than this is treated as a reset
	contourStepLo     = 2.0  // ideal gentle-build step: [lo, hi] costs nothing
	contourStepHi     = 12.0
	contourMinRunLen  = 3   // a build must span at least this many tracks to "qualify"
	contourMinRunRise = 8.0 // ...and rise at least this much for its reset to be free
	contourWaveLenLo  = 6.0 // fallback (no durations): target one wave per 6-10 tracks
	contourWaveLenHi  = 10.0

	waveMinutesLo = 18.0 // target one wave per ~18-30 minutes of music when durations known
	waveMinutesHi = 30.0

	contourBigJumpDiv    = 15.0 // divisor for over-large upward leaps within a build
	contourFlatPenalty   = 0.05 // near-flat step inside a build (mild anti-plateau)
	contourDipTol        = 2.0  // dips smaller than this count as flat
	contourDipDiv        = 16.0 // divisor for jittery dips inside a build
	contourJitterReset   = 1.0  // penalty for a reset that follows a non-qualifying build
	contourWaveCountUnit = 0.6  // penalty per wave outside the target band
)

// MixScore is the adaptive quality breakdown for an ordering.
type MixScore struct {
	Total         float64
	PerTrack      float64
	Transitions   int
	ActiveSignals []string

	HarmonicTotal float64
	TempoTotal    float64
	ValenceTotal  float64
	AcousticTotal float64
	ContourTotal  float64

	Contour ContourStats
	Worst   []TransitionDetail
}

// ContourStats explains the global energy-shape penalty.
type ContourStats struct {
	Builds            int
	Resets            int
	TargetResetLo     int
	TargetResetHi     int
	SmoothnessPenalty float64
	ResetPenalty      float64
	WaveCountPenalty  float64
	RawPenalty        float64
}

// TransitionDetail captures the per-transition coherence cost for reporting.
type TransitionDetail struct {
	Index     int
	FromTitle string
	ToTitle   string
	FromKey   track.Key
	ToKey     track.Key
	Harmonic  float64
	Tempo     float64
	Valence   float64
	Acoustic  float64
	Pairwise  float64
}

// ScoreMix scores an ordering with the default weights.
func ScoreMix(tracks []track.Track) MixScore {
	return ScoreMixWith(tracks, DefaultWeights)
}

// ScoreMixWith scores an ordering with explicit weights and returns a full breakdown.
func ScoreMixWith(tracks []track.Track, w Weights) MixScore {
	score := MixScore{ActiveSignals: activeSignals(tracks)}
	if len(tracks) <= 1 {
		return score
	}

	details := make([]TransitionDetail, 0, len(tracks)-1)
	for i := 0; i+1 < len(tracks); i++ {
		a, b := tracks[i], tracks[i+1]
		d := TransitionDetail{
			Index:     i,
			FromTitle: a.Title,
			ToTitle:   b.Title,
			FromKey:   a.Key,
			ToKey:     b.Key,
			Harmonic:  w.Harmonic * harmonicCost(a.Key, b.Key),
			Tempo:     w.Tempo * tempoCost(a.BPM, b.BPM),
			Valence:   w.Valence * valenceCost(a, b),
			Acoustic:  w.Acoustic * acousticCost(a, b),
		}
		d.Pairwise = d.Harmonic + d.Tempo + d.Valence + d.Acoustic

		score.HarmonicTotal += d.Harmonic
		score.TempoTotal += d.Tempo
		score.ValenceTotal += d.Valence
		score.AcousticTotal += d.Acoustic
		details = append(details, d)
	}

	minResets, maxResets := waveResetBand(tracks)
	score.Contour = contourPenalty(intensities(tracks), minResets, maxResets)
	score.ContourTotal = w.Contour * score.Contour.RawPenalty

	score.Transitions = len(details)
	score.Total = score.HarmonicTotal + score.TempoTotal + score.ValenceTotal +
		score.AcousticTotal + score.ContourTotal
	if score.Transitions > 0 {
		score.PerTrack = score.Total / float64(len(tracks))
	}
	score.Worst = worstTransitions(details, 8)
	return score
}

// coherenceCost is the pairwise cost of playing b after a.
func coherenceCost(a, b track.Track, w Weights) float64 {
	return w.Harmonic*harmonicCost(a.Key, b.Key) +
		w.Tempo*tempoCost(a.BPM, b.BPM) +
		w.Valence*valenceCost(a, b) +
		w.Acoustic*acousticCost(a, b)
}

// mixTotal computes just the total score of an ordering (no reporting breakdown). It
// equals ScoreMixWith(tracks, w).Total and is used where the value is needed many
// times, such as marginal-cost analysis.
func mixTotal(tracks []track.Track, w Weights) float64 {
	if len(tracks) <= 1 {
		return 0
	}
	total := 0.0
	for i := 0; i+1 < len(tracks); i++ {
		total += coherenceCost(tracks[i], tracks[i+1], w)
	}
	minResets, maxResets := waveResetBand(tracks)
	total += w.Contour * contourPenalty(intensities(tracks), minResets, maxResets).RawPenalty
	return total
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

// tempoCost folds tempo onto the octave circle so half/double-time pairs are treated
// as close, then costs the residual percentage difference.
func tempoCost(a, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	octaves := math.Log2(b / a)
	octaves -= math.Round(octaves) // fold to [-0.5, 0.5]
	pctDiff := math.Abs(math.Exp2(octaves)-1) * 100
	return math.Min(1.5, pctDiff/10.0)
}

// valenceCost penalizes large mood swings when both tracks report valence.
func valenceCost(a, b track.Track) float64 {
	if a.Valence == nil || b.Valence == nil {
		return 0
	}
	return math.Min(0.8, math.Abs(float64(*a.Valence-*b.Valence))/50.0)
}

// acousticCost penalizes abrupt acoustic/electronic swaps when both tracks report it.
func acousticCost(a, b track.Track) float64 {
	if a.Acousticness == nil || b.Acousticness == nil {
		return 0
	}
	return math.Min(0.8, math.Abs(float64(*a.Acousticness-*b.Acousticness))/70.0)
}

// contourPenalty scores the global intensity shape. Builds (rising runs) should be
// gradual; resets (drops beyond contourResetDrop) that follow a qualifying build are
// free; jittery dips, over-large leaps, and a reset count outside [minResets,
// maxResets] (the target wave cadence) are penalized. The ending is not scored.
func contourPenalty(vals []float64, minResets, maxResets int) ContourStats {
	stats := ContourStats{TargetResetLo: minResets, TargetResetHi: maxResets}
	n := len(vals)
	if n < 3 {
		return stats
	}

	buildStart := 0
	resets := 0
	smoothness := 0.0
	resetPenalty := 0.0

	for i := 0; i+1 < n; i++ {
		d := vals[i+1] - vals[i]

		if d <= -contourResetDrop {
			// Reset boundary: was the just-completed build a real build?
			runLen := i - buildStart + 1
			rise := vals[i] - vals[buildStart]
			if runLen < contourMinRunLen || rise < contourMinRunRise {
				resetPenalty += contourJitterReset
			}
			resets++
			buildStart = i + 1
			continue
		}

		switch {
		case d >= contourStepLo && d <= contourStepHi:
			// ideal gentle build step
		case d > contourStepHi:
			smoothness += (d - contourStepHi) / contourBigJumpDiv
		case d >= -contourDipTol && d < contourStepLo:
			smoothness += contourFlatPenalty
		default: // dip within a build (jitter)
			smoothness += (-d - contourDipTol) / contourDipDiv
		}
	}

	waveCountPenalty := 0.0
	switch {
	case resets < minResets:
		waveCountPenalty = float64(minResets-resets) * contourWaveCountUnit
	case resets > maxResets:
		waveCountPenalty = float64(resets-maxResets) * contourWaveCountUnit
	}

	stats.Builds = resets + 1
	stats.Resets = resets
	stats.SmoothnessPenalty = smoothness
	stats.ResetPenalty = resetPenalty
	stats.WaveCountPenalty = waveCountPenalty
	stats.RawPenalty = smoothness + resetPenalty + waveCountPenalty
	return stats
}

// waveResetBand returns the target [min,max] number of resets for a set. When most
// tracks report a duration, the cadence is time-based (a wave every ~18-30 min);
// otherwise it falls back to a track-count cadence (a wave every 6-10 tracks).
func waveResetBand(tracks []track.Track) (minResets, maxResets int) {
	n := len(tracks)
	if n == 0 {
		return 0, 0
	}

	var totalSec, have int
	for _, t := range tracks {
		if t.Duration != nil {
			totalSec += *t.Duration
			have++
		}
	}

	var minWaves, maxWaves int
	if have > n/2 && totalSec > 0 {
		if have < n { // estimate missing durations from the average
			totalSec += (totalSec / have) * (n - have)
		}
		totalMin := float64(totalSec) / 60.0
		minWaves = int(math.Round(totalMin / waveMinutesHi)) // longest waves -> fewest
		maxWaves = int(math.Round(totalMin / waveMinutesLo)) // shortest waves -> most
	} else {
		minWaves = int(math.Round(float64(n) / contourWaveLenHi))
		maxWaves = int(math.Round(float64(n) / contourWaveLenLo))
	}

	minResets = max(minWaves-1, 0)
	maxResets = max(maxWaves-1, minResets)
	return minResets, maxResets
}

// intensity blends energy with danceability (when present) into the value the
// contour is shaped around.
func intensity(t track.Track) float64 {
	e := float64(t.Energy)
	if t.Danceability != nil {
		return 0.7*e + 0.3*float64(*t.Danceability)
	}
	return e
}

func intensities(tracks []track.Track) []float64 {
	vals := make([]float64, len(tracks))
	for i, t := range tracks {
		vals[i] = intensity(t)
	}
	return vals
}

// activeSignals reports which optional signals are present in at least half the
// tracks (harmonic, tempo, and energy contour are always active).
func activeSignals(tracks []track.Track) []string {
	signals := []string{"harmonic", "tempo", "energy"}
	if len(tracks) == 0 {
		return signals
	}
	var dance, valence, acoustic int
	for _, t := range tracks {
		if t.Danceability != nil {
			dance++
		}
		if t.Valence != nil {
			valence++
		}
		if t.Acousticness != nil {
			acoustic++
		}
	}
	half := len(tracks) / 2
	if dance > half {
		signals = append(signals, "danceability")
	}
	if valence > half {
		signals = append(signals, "valence")
	}
	if acoustic > half {
		signals = append(signals, "acousticness")
	}
	return signals
}

func worstTransitions(details []TransitionDetail, limit int) []TransitionDetail {
	sorted := make([]TransitionDetail, len(details))
	copy(sorted, details)
	sortByPairwiseDesc(sorted)
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted
}

func sortByPairwiseDesc(details []TransitionDetail) {
	for i := 1; i < len(details); i++ {
		for j := i; j > 0 && details[j].Pairwise > details[j-1].Pairwise; j-- {
			details[j], details[j-1] = details[j-1], details[j]
		}
	}
}
