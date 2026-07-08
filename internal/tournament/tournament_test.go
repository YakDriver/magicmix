package tournament

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/YakDriver/magicmix/internal/track"
)

// vibe builds n tracks of one vibe. Titles are prefix+index; a higher index encodes a
// higher hidden "quality" the judge prefers, giving a transitive preference within a
// vibe without touching the fields that determine tags.
func vibe(prefix string, n, energy int, bpm float64, valence, pop, dance, aco int) []track.Track {
	dur := 210
	out := make([]track.Track, n)
	for i := range n {
		v, p, d, a := valence, pop, dance, aco
		out[i] = track.Track{
			Title:        prefix + pad(i),
			Artist:       prefix,
			BPM:          bpm,
			Energy:       energy,
			Key:          track.Key{Number: 8, Mode: track.ModeA},
			Duration:     &dur,
			Valence:      &v,
			Popularity:   &p,
			Danceability: &d,
			Acousticness: &a,
		}
	}
	return out
}

func pad(i int) string {
	s := strconv.Itoa(i)
	if len(s) < 2 {
		return "0" + s
	}
	return s
}

func quality(t track.Track) int {
	n, _ := strconv.Atoi(t.Title[len(t.Title)-2:])
	return n
}

// preferHigherQuality is a transitive judge: it keeps the higher-quality song.
func preferHigherQuality(m Matchup) Outcome {
	if quality(m.Left) >= quality(m.Right) {
		return PickLeft
	}
	return PickRight
}

func mixedList() []track.Track {
	bangers := vibe("b", 20, 90, 130, 80, 90, 80, 10)
	mellow := vibe("m", 10, 30, 90, 30, 40, 30, 80)
	return append(bangers, mellow...)
}

func countPrefix(tracks []track.Track, prefix string) int {
	n := 0
	for _, t := range tracks {
		if strings.HasPrefix(t.Title, prefix) {
			n++
		}
	}
	return n
}

func TestTournamentFillsBudget(t *testing.T) {
	tracks := mixedList()
	res, err := Run(context.Background(), tracks, preferHigherQuality, Config{TargetMinutes: 45.5, Variety: 0.5, Seed: 1})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(res.Kept) != 13 {
		t.Fatalf("kept %d tracks, want 13 (45.5 min / 3.5 min)", len(res.Kept))
	}
	if len(res.Kept)+len(res.Cut) != len(tracks) {
		t.Fatalf("kept+cut = %d, want %d", len(res.Kept)+len(res.Cut), len(tracks))
	}
}

func TestTournamentDeterministic(t *testing.T) {
	tracks := mixedList()
	cfg := Config{TargetMinutes: 45.5, Variety: 0.5, Seed: 7}
	a, _ := Run(context.Background(), tracks, preferHigherQuality, cfg)
	b, _ := Run(context.Background(), tracks, preferHigherQuality, cfg)
	if titles(a.Kept) != titles(b.Kept) {
		t.Fatalf("non-deterministic keep-set:\n %s\n %s", titles(a.Kept), titles(b.Kept))
	}
	if a.Comparisons != b.Comparisons {
		t.Fatalf("non-deterministic comparison count: %d vs %d", a.Comparisons, b.Comparisons)
	}
}

func TestTournamentFewerComparisonsThanSort(t *testing.T) {
	tracks := mixedList()
	res, _ := Run(context.Background(), tracks, preferHigherQuality, Config{TargetMinutes: 45.5, Variety: 0.5, Seed: 3})
	allPairs := len(tracks) * (len(tracks) - 1) / 2 // 435
	if res.Comparisons <= 0 || res.Comparisons >= allPairs {
		t.Fatalf("comparisons %d should be positive and well under all-pairs %d", res.Comparisons, allPairs)
	}
}

func TestTournamentVarietyParesDownCommonVibe(t *testing.T) {
	tracks := mixedList()
	low, _ := Run(context.Background(), tracks, preferHigherQuality, Config{TargetMinutes: 45.5, Variety: 0, Seed: 5})
	high, _ := Run(context.Background(), tracks, preferHigherQuality, Config{TargetMinutes: 45.5, Variety: 5, Seed: 5})

	lowMellow := countPrefix(low.Kept, "m")
	highMellow := countPrefix(high.Kept, "m")
	if highMellow < lowMellow {
		t.Fatalf("higher variety kept fewer of the rare vibe: high=%d low=%d", highMellow, lowMellow)
	}
	if highMellow < 4 {
		t.Fatalf("high variety should protect the rare vibe, kept only %d mellow", highMellow)
	}
	// The over-represented vibe must be pared: not all 20 bangers can be kept anyway
	// (budget 13), but high variety should keep noticeably fewer bangers than low.
	if countPrefix(high.Kept, "b") > countPrefix(low.Kept, "b") {
		t.Fatalf("high variety kept more of the common vibe than low variety")
	}
}

func TestTournamentQuitReturnsPartial(t *testing.T) {
	tracks := mixedList()
	n := 0
	quitAfter := func(m Matchup) Outcome {
		n++
		if n > 10 {
			return Quit
		}
		return preferHigherQuality(m)
	}
	res, err := Run(context.Background(), tracks, quitAfter, Config{TargetMinutes: 45.5, Variety: 0.5, Seed: 2})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !res.Aborted {
		t.Fatal("expected Aborted=true after Quit")
	}
	if res.Comparisons != 10 {
		t.Fatalf("expected 10 judged comparisons before quit, got %d", res.Comparisons)
	}
	if len(res.Kept) == 0 {
		t.Fatal("quit should still return a best-guess keep-set")
	}
}

func TestTournamentSkipTerminates(t *testing.T) {
	tracks := mixedList()
	res, err := Run(context.Background(), tracks, func(Matchup) Outcome { return Skip }, Config{TargetMinutes: 45.5, Variety: 0.5, Seed: 4})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(res.Kept) != 13 {
		t.Fatalf("kept %d, want 13 even when every battle is skipped", len(res.Kept))
	}
}

func TestTournamentEmptyInput(t *testing.T) {
	res, err := Run(context.Background(), nil, preferHigherQuality, Config{TargetMinutes: 30, Variety: 1})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(res.Kept) != 0 || len(res.Cut) != 0 {
		t.Fatal("empty input should yield empty result")
	}
}

func titles(tracks []track.Track) string {
	names := make([]string, len(tracks))
	for i, t := range tracks {
		names[i] = t.Title
	}
	// stable order for comparison
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return strings.Join(names, ",")
}

func TestTournamentBothAwardsBothAWin(t *testing.T) {
	tracks := vibe("b", 2, 90, 130, 80, 90, 80, 10)
	res, err := Run(context.Background(), tracks, func(Matchup) Outcome { return PickBoth }, Config{TargetMinutes: 7, Variety: 0.5, Seed: 1})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	for _, c := range res.Cut {
		if c.Losses > 0 {
			t.Fatalf("PickBoth should record no losses, got %d-%d", c.Wins, c.Losses)
		}
	}
}

func TestTournamentCutReasonsAreHonest(t *testing.T) {
	tracks := mixedList()
	res, err := Run(context.Background(), tracks, preferHigherQuality, Config{TargetMinutes: 45.5, Variety: 3, Seed: 9})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	for _, c := range res.Cut {
		switch c.Reason {
		case CutRedundant:
			if c.Wins <= c.Losses {
				t.Fatalf("redundant cut %q needs a winning record, got %d-%d", c.Track.Title, c.Wins, c.Losses)
			}
			if c.SharedKept == 0 {
				t.Fatalf("redundant cut %q should have kept vibe-neighbors, got 0", c.Track.Title)
			}
		case CutLost:
			if c.Wins > c.Losses {
				t.Fatalf("lost cut %q should not have a winning record, got %d-%d", c.Track.Title, c.Wins, c.Losses)
			}
		}
	}
}

func TestTournamentCanceledContextPropagates(t *testing.T) {
	tracks := mixedList()
	ctx, cancel := context.WithCancel(context.Background())
	judge := func(m Matchup) Outcome {
		cancel() // cancel mid-run, then keep answering
		return preferHigherQuality(m)
	}
	_, err := Run(ctx, tracks, judge, Config{TargetMinutes: 45.5, Variety: 0.5, Seed: 1})
	if err == nil {
		t.Fatal("expected a context error when the context is canceled mid-run")
	}
}

func TestTournamentTieRecordsAreNotLost(t *testing.T) {
	// Every battle is skipped, so no song has a losing (or winning) record. None of
	// the cuts should be labeled "lost" — a tie is not a loss.
	tracks := mixedList()
	res, err := Run(context.Background(), tracks, func(Matchup) Outcome { return Skip }, Config{TargetMinutes: 45.5, Variety: 0.5, Seed: 1})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	for _, c := range res.Cut {
		if c.Reason == CutLost {
			t.Fatalf("tie-record cut %q (%d-%d) labeled lost", c.Track.Title, c.Wins, c.Losses)
		}
	}
}
