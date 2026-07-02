package strategy

import (
	"context"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/YakDriver/magicmix/internal/track"
)

const chaveStrategyName = "chave"

const (
	chaveMinSize        = 6    // a group must have at least this many songs to form a chave
	chaveTargetMinutes  = 22.0 // aim for ~20-30 min chaves
	chaveMaxMinutes     = 30.0 // hard ceiling on chave length
	chaveMaxTracks      = 12   // safety cap on tracks per chave
	chaveFallbackTracks = 8    // chave size in tracks when durations are unavailable
	chaveTagCooldown    = 2    // used tags sit out this many chaves
	chaveComboRetries   = 60   // attempts to find a populated 3-tag group
)

// ChaveSorter builds a set from "chaves" — chapters/waves with a theme. A chave is a
// ~20-30 minute run of songs that share three tags drawn from three different signals
// (e.g. "modern + upbeat + danceable"), ordered to flow smoothly and build in
// intensity. Multi-signal grouping reads as a coherent human vibe rather than the
// choppy feel of a single-signal grouping.
//
// Tags come from median splits of each signal (energy, danceability, valence,
// acousticness, popularity, bpm) plus era buckets, so the grouping adapts to the
// source list. After a chave, its three tags cool down for a couple of chaves so the
// next one draws from different signals, keeping the set changing character.
type ChaveSorter struct{}

func NewChaveSorter() *ChaveSorter {
	return &ChaveSorter{}
}

func (s *ChaveSorter) Name() string {
	return chaveStrategyName
}

func (s *ChaveSorter) Sort(ctx context.Context, tracks []track.Track) ([]track.Track, error) {
	pool := make([]track.Track, len(tracks))
	for i, t := range tracks {
		pool[i] = t.Clone()
	}

	seed, ok := seedFromContext(ctx)
	if !ok || seed == 0 {
		seed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seed))

	chaves, err := composeChaves(ctx, pool, rng)
	if err != nil {
		return nil, err
	}
	out := make([]track.Track, 0, len(pool))
	for _, ch := range chaves {
		out = append(out, ch...)
	}
	return out, nil
}

// composeChaves carves the pool into themed, intensity-building chaves.
func composeChaves(ctx context.Context, pool []track.Track, rng *rand.Rand) ([][]track.Track, error) {
	tagger, signals := newTagger(pool)
	cooldown := map[string]int{}
	var chaves [][]track.Track

	for len(pool) >= chaveMinSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for tag := range cooldown {
			if cooldown[tag] > 0 {
				cooldown[tag]--
			}
		}

		combo, ok := pickCombo(pool, tagger, signals, cooldown, rng)
		if !ok {
			break
		}
		members, rest := partitionByCombo(pool, tagger, combo)
		wave, leftover := drawWave(members, rng)

		chave, err := flowChave(ctx, wave)
		if err != nil {
			return nil, err
		}
		chaves = append(chaves, chave)

		pool = append(rest, leftover...)
		for _, tag := range combo {
			cooldown[tag] = chaveTagCooldown
		}
	}

	// Remainder: break whatever is left into bounded, flowed closing chaves.
	for len(pool) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wave, leftover := drawWave(pool, rng)
		chave, err := flowChave(ctx, wave)
		if err != nil {
			return nil, err
		}
		chaves = append(chaves, chave)
		pool = leftover
	}
	return chaves, nil
}

// flowChave orders a chave for smooth flow (harmonic/tempo/mood) and an intensity
// build. It runs the flow optimizer, then falls back to a plain intensity ramp if the
// flowed order does not build — so a chave always rises from calm to peak.
func flowChave(ctx context.Context, wave []track.Track) ([]track.Track, error) {
	ordered, err := NewFlowSorter().Sort(ctx, wave)
	if err != nil {
		return nil, err
	}
	if !buildsWell(ordered) {
		buildOrder(ordered)
	}
	return ordered, nil
}

// buildsWell reports whether a chave's second half is at least as intense as its
// first half (a build, not a fade).
func buildsWell(chave []track.Track) bool {
	if len(chave) < 4 {
		return true
	}
	h := len(chave) / 2
	return meanIntensity(chave[h:]) >= meanIntensity(chave[:h])
}

func meanIntensity(tracks []track.Track) float64 {
	if len(tracks) == 0 {
		return 0
	}
	sum := 0.0
	for _, t := range tracks {
		sum += intensity(t)
	}
	return sum / float64(len(tracks))
}

// buildOrder sorts a chave so intensity rises from start to finish.
func buildOrder(chave []track.Track) {
	sort.SliceStable(chave, func(a, b int) bool {
		return intensity(chave[a]) < intensity(chave[b])
	})
}

// newTagger computes median-split tags for a track list. It returns a function that
// maps a track to its tag per signal, plus the set of possible tags per signal.
func newTagger(tracks []track.Track) (func(track.Track) map[string]string, map[string][]string) {
	getters := map[string]func(track.Track) (float64, bool){
		"nrg": func(t track.Track) (float64, bool) { return float64(t.Energy), true },
		"bpm": func(t track.Track) (float64, bool) { return t.BPM, t.BPM > 0 },
		"dnc": func(t track.Track) (float64, bool) { return optFloat(t.Danceability) },
		"val": func(t track.Track) (float64, bool) { return optFloat(t.Valence) },
		"aco": func(t track.Track) (float64, bool) { return optFloat(t.Acousticness) },
		"pop": func(t track.Track) (float64, bool) { return optFloat(t.Popularity) },
	}

	median := map[string]float64{}
	for name, get := range getters {
		var vals []float64
		for _, t := range tracks {
			if v, ok := get(t); ok {
				vals = append(vals, v)
			}
		}
		if len(vals) > 0 {
			median[name] = medianOf(vals)
		}
	}

	signals := map[string][]string{}
	for name := range median {
		signals[name] = []string{name + ":hi", name + ":lo"}
	}
	hasYear := false
	for _, t := range tracks {
		if t.Year != nil {
			hasYear = true
			break
		}
	}
	if hasYear {
		signals["era"] = []string{"era:old", "era:mid", "era:new"}
	}

	tagger := func(t track.Track) map[string]string {
		tags := make(map[string]string, len(signals))
		for name, get := range getters {
			med, ok := median[name]
			if !ok {
				continue
			}
			if v, present := get(t); present {
				if v >= med {
					tags[name] = name + ":hi"
				} else {
					tags[name] = name + ":lo"
				}
			}
		}
		if t.Year != nil {
			switch {
			case *t.Year < 1990:
				tags["era"] = "era:old"
			case *t.Year < 2010:
				tags["era"] = "era:mid"
			default:
				tags["era"] = "era:new"
			}
		}
		return tags
	}
	return tagger, signals
}

// pickCombo finds three tags from three different signals whose conjunction has at
// least chaveMinSize songs in the pool, avoiding tags currently on cooldown.
func pickCombo(pool []track.Track, tagger func(track.Track) map[string]string, signals map[string][]string, cooldown map[string]int, rng *rand.Rand) ([]string, bool) {
	poolTags := make([]map[string]string, len(pool))
	for i, t := range pool {
		poolTags[i] = tagger(t)
	}

	sigNames := make([]string, 0, len(signals))
	for name := range signals {
		sigNames = append(sigNames, name)
	}
	sort.Strings(sigNames) // deterministic base order before the seeded shuffle

	for range chaveComboRetries {
		rng.Shuffle(len(sigNames), func(i, j int) { sigNames[i], sigNames[j] = sigNames[j], sigNames[i] })

		var combo []string
		for _, name := range sigNames {
			available := make([]string, 0, len(signals[name]))
			for _, tag := range signals[name] {
				if cooldown[tag] == 0 {
					available = append(available, tag)
				}
			}
			if len(available) == 0 {
				continue
			}
			combo = append(combo, available[rng.Intn(len(available))])
			if len(combo) == 3 {
				break
			}
		}
		if len(combo) < 3 {
			continue
		}

		count := 0
		for _, tags := range poolTags {
			if comboMatches(tags, combo) {
				count++
			}
		}
		if count >= chaveMinSize {
			return combo, true
		}
	}
	return nil, false
}

func comboMatches(tags map[string]string, combo []string) bool {
	for _, tag := range combo {
		if tags[signalOf(tag)] != tag {
			return false
		}
	}
	return true
}

func signalOf(tag string) string {
	if before, _, ok := strings.Cut(tag, ":"); ok {
		return before
	}
	return tag
}

func partitionByCombo(pool []track.Track, tagger func(track.Track) map[string]string, combo []string) (members, rest []track.Track) {
	for _, t := range pool {
		if comboMatches(tagger(t), combo) {
			members = append(members, t)
		} else {
			rest = append(rest, t)
		}
	}
	return members, rest
}

// drawWave randomly selects ~one chave's worth of songs (at least chaveMinSize, about
// chaveTargetMinutes of playtime, capped by chaveMaxTracks/chaveMaxMinutes; or
// chaveFallbackTracks songs when durations are unavailable) and returns them plus the
// unused remainder.
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
		if len(chosen) >= chaveMaxTracks {
			break
		}
		var dur float64
		if haveDurations {
			dur = float64(*members[idx].Duration)
		}
		// The ~30-min ceiling is a hard cap: it wins over the min-size preference.
		if haveDurations && len(chosen) > 0 && secs+dur > chaveMaxMinutes*60 {
			break
		}
		// Once we have a full chave (>= min size and target length), stop.
		if len(chosen) >= chaveMinSize {
			if haveDurations && secs >= chaveTargetMinutes*60 {
				break
			}
			if !haveDurations && len(chosen) >= chaveFallbackTracks {
				break
			}
		}
		secs += dur
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

func optFloat(p *int) (float64, bool) {
	if p == nil {
		return 0, false
	}
	return float64(*p), true
}

func medianOf(vals []float64) float64 {
	s := make([]float64, len(vals))
	copy(s, vals)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
