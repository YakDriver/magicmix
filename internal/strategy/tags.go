package strategy

import (
	"sort"

	"github.com/YakDriver/magicmix/internal/track"
)

// TagSets returns, for each track, the sorted set of median-split multi-signal tags —
// the same tags the chave strategy groups by: hi/lo splits of energy, bpm,
// danceability, valence, acousticness, and popularity, plus an era bucket. The splits
// adapt to the given list, and signals absent from a track contribute no tag. The
// result is index-aligned with tracks; callers use shared-tag counts as a graded
// similarity between songs.
func TagSets(tracks []track.Track) [][]string {
	tagger, _ := newTagger(tracks)
	out := make([][]string, len(tracks))
	for i, t := range tracks {
		m := tagger(t)
		tags := make([]string, 0, len(m))
		for _, v := range m {
			tags = append(tags, v)
		}
		sort.Strings(tags)
		out[i] = tags
	}
	return out
}
