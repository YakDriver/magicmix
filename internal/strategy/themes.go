package strategy

import (
	"context"
	"math/rand"
	"time"

	"github.com/YakDriver/magicmix/internal/track"
)

const themesStrategyName = "themes"

const (
	themeMinWaveTracks = 5    // a pot needs at least this many songs to yield a wave
	themeWaveMinutes   = 24.0 // aim for roughly one ~20-min wave per chunk
	themeWaveTracks    = 8    // fallback chunk size when durations are absent
	themePotsPerRound  = 4    // upper bound on pots drawn per round
)

// ThemesSorter builds a set out of themed waves. Each round it draws a random subset
// of "similarity pots" (bands of a characteristic — mood, danceability, era, etc.),
// runs the pool members of each pot through the flow optimizer, and emits the leading
// ~one-wave chunk as a coherent themed section. Unused songs return to the pool and
// may be picked up by a different pot later; when the pool is too small to theme, the
// leftovers are flowed as a final chapter.
//
// Energy/intensity still provides the build within each wave (via flow); the theme
// only decides which songs cluster together. The random pot subset per seed makes the
// chaptering vary run to run.
type ThemesSorter struct {
	weights Weights
}

func NewThemesSorter() *ThemesSorter {
	return &ThemesSorter{weights: DefaultWeights}
}

func (s *ThemesSorter) Name() string {
	return themesStrategyName
}

type themePot struct {
	name  string
	match func(track.Track) bool
}

func (s *ThemesSorter) Sort(ctx context.Context, tracks []track.Track) ([]track.Track, error) {
	pool := make([]track.Track, len(tracks))
	for i, t := range tracks {
		pool[i] = t.Clone()
	}
	if len(pool) <= themeMinWaveTracks {
		return s.flow(ctx, pool)
	}

	seed, ok := seedFromContext(ctx)
	if !ok || seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	pots := availablePots(pool)
	out := make([]track.Track, 0, len(pool))

	for len(pool) >= themeMinWaveTracks && len(pots) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		progressed := false
		for _, p := range pickPots(pots, rng) {
			members, rest := partition(pool, p.match)
			if len(members) < themeMinWaveTracks {
				continue
			}
			ordered, err := s.flow(ctx, members)
			if err != nil {
				return nil, err
			}
			take := waveChunkSize(ordered)
			out = append(out, ordered[:take]...)
			pool = append(rest, ordered[take:]...)
			progressed = true
		}
		if !progressed {
			break
		}
	}

	// Wind-down: flow whatever is left as a final chapter.
	if len(pool) > 0 {
		rest, err := s.flow(ctx, pool)
		if err != nil {
			return nil, err
		}
		out = append(out, rest...)
	}
	return out, nil
}

func (s *ThemesSorter) flow(ctx context.Context, tracks []track.Track) ([]track.Track, error) {
	return (&FlowSorter{weights: s.weights}).Sort(ctx, tracks)
}

// availablePots returns the pots whose characteristic is present in enough tracks to
// form at least one wave.
func availablePots(tracks []track.Track) []themePot {
	catalog := []themePot{
		{"happy", func(t track.Track) bool { return t.Valence != nil && *t.Valence >= 60 }},
		{"moody", func(t track.Track) bool { return t.Valence != nil && *t.Valence <= 40 }},
		{"danceable", func(t track.Track) bool { return t.Danceability != nil && *t.Danceability >= 65 }},
		{"lowkey", func(t track.Track) bool { return t.Danceability != nil && *t.Danceability <= 45 }},
		{"acoustic", func(t track.Track) bool { return t.Acousticness != nil && *t.Acousticness >= 50 }},
		{"electronic", func(t track.Track) bool { return t.Acousticness != nil && *t.Acousticness <= 20 }},
		{"modern", func(t track.Track) bool { return t.Year != nil && *t.Year >= 2020 }},
		{"throwback", func(t track.Track) bool { return t.Year != nil && *t.Year <= 2010 }},
		{"high-energy", func(t track.Track) bool { return t.Energy >= 70 }},
		{"mellow", func(t track.Track) bool { return t.Energy <= 45 }},
	}

	avail := make([]themePot, 0, len(catalog))
	for _, p := range catalog {
		count := 0
		for _, t := range tracks {
			if p.match(t) {
				count++
			}
		}
		if count >= themeMinWaveTracks {
			avail = append(avail, p)
		}
	}
	return avail
}

// pickPots returns a seeded random subset (1..themePotsPerRound) of the pots.
func pickPots(pots []themePot, rng *rand.Rand) []themePot {
	shuffled := make([]themePot, len(pots))
	copy(shuffled, pots)
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	k := 1 + rng.Intn(min(themePotsPerRound, len(shuffled)))
	return shuffled[:k]
}

func partition(pool []track.Track, match func(track.Track) bool) (members, rest []track.Track) {
	for _, t := range pool {
		if match(t) {
			members = append(members, t)
		} else {
			rest = append(rest, t)
		}
	}
	return members, rest
}

// waveChunkSize returns how many leading tracks of a flowed pot make ~one wave:
// enough to reach ~themeWaveMinutes of playtime (or themeWaveTracks without
// durations), clamped to [themeMinWaveTracks, len].
func waveChunkSize(ordered []track.Track) int {
	n := len(ordered)
	target := themeWaveTracks

	haveDurations := true
	for _, t := range ordered {
		if t.Duration == nil {
			haveDurations = false
			break
		}
	}
	if haveDurations {
		target = n
		acc := 0.0
		for i, t := range ordered {
			acc += float64(*t.Duration)
			if acc >= themeWaveMinutes*60 {
				target = i + 1
				break
			}
		}
	}

	if target < themeMinWaveTracks {
		target = themeMinWaveTracks
	}
	if target > n {
		target = n
	}
	return target
}
