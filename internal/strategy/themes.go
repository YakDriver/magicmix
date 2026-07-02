package strategy

import (
	"context"
	"math/rand"
	"sort"
	"time"

	"github.com/YakDriver/magicmix/internal/track"
)

const themesStrategyName = "themes"

const (
	themeMinChapter    = 4    // a theme needs at least this many pool songs to form a chapter
	themeTargetMinutes = 22.0 // aim for ~20-30 min chapters
	themeMaxMinutes    = 30.0 // hard ceiling on chapter length
	themeMaxTracks     = 12   // safety cap (and the size when durations are absent, see below)
	themeFallbackTrack = 8    // chapter size in tracks when durations are unavailable
)

// ThemesSorter builds a set from themed chapters ("mini-sets"). Each chapter is a
// ~20-30 minute run of songs that share a characteristic (mood, danceability,
// acousticness, era, or energy band) and that builds in intensity from calm to peak.
// Consecutive chapters use different themes so the set keeps changing character.
//
// Composition is direct and bounded: pick a theme (different from the last), draw a
// ~one-wave-worth of its songs from the pool, order them by intensity ascending
// (the build), and move on. Leftover songs return to the pool for other themes; the
// final remainder becomes a closing chapter. The random draws make the chaptering
// vary by seed.
type ThemesSorter struct{}

func NewThemesSorter() *ThemesSorter {
	return &ThemesSorter{}
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

	seed, ok := seedFromContext(ctx)
	if !ok || seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	out := make([]track.Track, 0, len(pool))
	for _, chapter := range composeChapters(pool, rng) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out = append(out, chapter...)
	}
	return out, nil
}

// composeChapters greedily carves the pool into themed, intensity-building chapters.
func composeChapters(pool []track.Track, rng *rand.Rand) [][]track.Track {
	var chapters [][]track.Track
	prevTheme := ""

	for len(pool) >= themeMinChapter {
		pots := availablePots(pool)
		if len(pots) == 0 {
			break
		}
		theme := pickTheme(pots, prevTheme, rng)
		members, rest := partition(pool, theme.match)

		wave, leftover := drawWave(members, rng)
		buildOrder(wave)
		chapters = append(chapters, wave)

		pool = append(rest, leftover...)
		prevTheme = theme.name
	}

	if len(pool) > 0 {
		buildOrder(pool)
		chapters = append(chapters, pool)
	}
	return chapters
}

// availablePots returns pots with at least themeMinChapter matching tracks.
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
		if count >= themeMinChapter {
			avail = append(avail, p)
		}
	}
	return avail
}

// pickTheme chooses a pot at random, preferring one different from the previous
// chapter's theme so consecutive chapters contrast.
func pickTheme(pots []themePot, prev string, rng *rand.Rand) themePot {
	candidates := pots
	if prev != "" {
		filtered := make([]themePot, 0, len(pots))
		for _, p := range pots {
			if p.name != prev {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) > 0 {
			candidates = filtered
		}
	}
	return candidates[rng.Intn(len(candidates))]
}

// drawWave randomly selects ~one chapter's worth of songs (about themeTargetMinutes
// of playtime, capped by themeMaxTracks; or themeFallbackTrack songs when durations
// are unavailable) and returns them plus the unused remainder.
func drawWave(members []track.Track, rng *rand.Rand) (wave, leftover []track.Track) {
	order := rng.Perm(len(members))

	haveDurations := true
	for _, t := range members {
		if t.Duration == nil {
			haveDurations = false
			break
		}
	}

	chosen := make(map[int]struct{})
	secs := 0.0
	for _, idx := range order {
		if len(chosen) >= themeMaxTracks {
			break
		}
		if haveDurations {
			if secs >= themeTargetMinutes*60 {
				break
			}
			next := secs + float64(*members[idx].Duration)
			// Don't blow past the hard ceiling once we already have a chapter.
			if len(chosen) >= themeMinChapter && next > themeMaxMinutes*60 {
				break
			}
			secs = next
		} else if len(chosen) >= themeFallbackTrack {
			break
		}
		chosen[idx] = struct{}{}
	}

	for i, t := range members {
		if _, ok := chosen[i]; ok {
			wave = append(wave, t)
		} else {
			leftover = append(leftover, t)
		}
	}
	return wave, leftover
}

// buildOrder sorts a chapter so intensity rises from start to finish.
func buildOrder(chapter []track.Track) {
	sort.SliceStable(chapter, func(a, b int) bool {
		return intensity(chapter[a]) < intensity(chapter[b])
	})
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
