// Package tournament selects which songs to keep for a time-bounded set through
// pairwise human auditions. It is a selection problem, not a ranking one: it never
// needs a champion or a full order, only a keep/cut partition, which lets it get away
// with far fewer comparisons than a sort.
//
// Songs audition Swiss-style — each battle pairs songs with a similar win/loss record
// and a similar vibe (many shared tags), so every judgment is a fair, like-vs-like
// choice. Records feed a diversity-aware selection: a song's value is its record minus
// a redundancy penalty that grows as the keep-set fills with songs sharing its tags.
// So over-represented vibes get pared down — the best few survive and the rest are cut
// as redundant — while rare vibes sail in. The comparison effort self-concentrates on
// the contested slots of common vibes, which is exactly where human judgment is needed.
package tournament

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/YakDriver/magicmix/internal/strategy"
	"github.com/YakDriver/magicmix/internal/track"
)

// Outcome is a judge's verdict on a single matchup.
type Outcome int

const (
	PickLeft  Outcome = iota // keep the left song over the right
	PickRight                // keep the right song over the left
	Skip                     // no opinion; record nothing, don't ask this pair again
	Quit                     // stop the tournament and keep the current best guess
	PickBoth                 // both are strong; award each a win and let diversity decide
)

// Phase labels where a battle sits in the process, for signposting.
type Phase string

const (
	PhaseSwiss  Phase = "Swiss"
	PhaseBubble Phase = "Bubble"
)

// Matchup is one battle presented to the judge.
type Matchup struct {
	Left, Right  track.Track
	Phase        Phase
	Battle       int // 1-based count of battles asked so far (including this one)
	Contested    int // songs whose keep/cut status is still unresolved
	KeepEstimate int // songs currently expected to be kept
	KeepTarget   int // songs the set aims to keep
}

// Judge decides a matchup. The interactive UI implements it with a keypress; tests
// implement it deterministically.
type Judge func(Matchup) Outcome

// Config controls a tournament run.
type Config struct {
	TargetMinutes float64 // desired set length; converted to a keep count
	Variety       float64 // diversity knob: how hard redundant songs are penalized
	Seed          int64   // deterministic pairing (0 = time-based)
}

// CutReason explains why a song did not make the keep-set.
type CutReason string

const (
	CutLost      CutReason = "lost"      // lost more auditions than it won
	CutMissed    CutReason = "missed"    // a decent record, but it just didn't fit the time budget
	CutRedundant CutReason = "redundant" // its record would have kept it, but its vibe was already covered
)

// Cut is a song that was left out, with the reason.
type Cut struct {
	Track        track.Track
	Reason       CutReason
	Wins, Losses int
	SharedKept   int // kept songs sharing this song's vibe (context for "redundant")
}

// Result is the outcome of a tournament.
type Result struct {
	Kept        []track.Track
	Cut         []Cut
	Comparisons int  // decisive battles judged (skips excluded)
	Aborted     bool // the judge quit early
	KeepTarget  int
}

const (
	restMargin    = 3 // |wins-losses| at which a song stops being scheduled in Swiss
	maxSwiss      = 6 // hard cap on Swiss rounds
	bubbleRounds  = 4 // hard cap on bubble rounds
	stableRepeats = 2 // stop Swiss once the keep-set is unchanged this many rounds
	avgSongMin    = 3.5
)

type engine struct {
	tracks []track.Track
	tags   [][]string
	dur    []float64 // seconds; nil-safe: 0 when unknown
	hasDur bool

	wins   []int
	losses []int
	played map[[2]int]bool

	target  float64 // seconds budget, or keep count when durations are absent
	variety float64
	rng     *rand.Rand

	judge   Judge
	battles int
	aborted bool
}

// Run auditions tracks and returns the keep/cut partition.
func Run(ctx context.Context, tracks []track.Track, judge Judge, cfg Config) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	e := &engine{
		tracks:  tracks,
		tags:    strategy.TagSets(tracks),
		played:  map[[2]int]bool{},
		wins:    make([]int, len(tracks)),
		losses:  make([]int, len(tracks)),
		variety: cfg.Variety,
		rng:     rand.New(rand.NewSource(seed)),
		judge:   judge,
	}

	e.dur = make([]float64, len(tracks))
	e.hasDur = len(tracks) > 0
	for i, t := range tracks {
		if t.Duration != nil {
			e.dur[i] = float64(*t.Duration)
		} else {
			e.hasDur = false
		}
	}
	if e.hasDur {
		e.target = cfg.TargetMinutes * 60
	} else {
		e.target = math.Round(cfg.TargetMinutes / avgSongMin) // keep count
	}

	if len(tracks) == 0 {
		return Result{}, nil
	}

	e.runSwiss(ctx)
	if !e.aborted {
		e.runBubble(ctx)
	}
	return e.finalize(), nil
}

// runSwiss battles songs until the keep-set stabilizes. The first round pairs everyone
// once to get an initial record; after that it only battles the contested band —
// songs near the keep/cut line — so effort concentrates where the decision is actually
// in doubt instead of re-auditioning obvious keeps and obvious cuts.
func (e *engine) runSwiss(ctx context.Context) {
	prevKept := map[int]bool{}
	stable := 0
	for round := range maxSwiss {
		if ctx.Err() != nil {
			return
		}
		var active []int
		if round == 0 {
			active = e.allIndices()
		} else {
			active = e.contestedBand(e.selectKeep())
		}
		active = e.notDecisive(active)
		pairs := e.pair(active)
		if len(pairs) == 0 {
			return
		}
		if !e.battleAll(ctx, pairs, PhaseSwiss) {
			return // quit
		}

		kept := e.selectKeep()
		keptSet := indexSet(kept)
		if sameSet(keptSet, prevKept) {
			stable++
			if stable >= stableRepeats {
				return
			}
		} else {
			stable = 0
		}
		prevKept = keptSet
	}
}

func (e *engine) allIndices() []int {
	ids := make([]int, len(e.tracks))
	for i := range ids {
		ids[i] = i
	}
	return ids
}

// notDecisive drops songs whose record is already lopsided enough to rest.
func (e *engine) notDecisive(ids []int) []int {
	out := ids[:0:0]
	for _, i := range ids {
		if abs(e.margin(i)) < restMargin {
			out = append(out, i)
		}
	}
	return out
}

// runBubble concentrates remaining battles on songs straddling the keep/cut line.
func (e *engine) runBubble(ctx context.Context) {
	for range bubbleRounds {
		if ctx.Err() != nil {
			return
		}
		kept := e.selectKeep()
		band := e.contestedBand(kept)
		if len(band) < 2 {
			return
		}
		pairs := e.pair(band)
		if len(pairs) == 0 {
			return
		}
		before := indexSet(kept)
		if !e.battleAll(ctx, pairs, PhaseBubble) {
			return // quit
		}
		if sameSet(indexSet(e.selectKeep()), before) {
			return // battles no longer move the keep-set
		}
	}
}

func (e *engine) battleAll(ctx context.Context, pairs [][2]int, phase Phase) bool {
	for _, p := range pairs {
		if ctx.Err() != nil {
			return false
		}
		kept := e.selectKeep()
		e.battles++
		out := e.judge(Matchup{
			Left:         e.tracks[p[0]],
			Right:        e.tracks[p[1]],
			Phase:        phase,
			Battle:       e.battles,
			Contested:    len(e.contestedBand(kept)),
			KeepEstimate: len(kept),
			KeepTarget:   e.keepTarget(),
		})
		switch out {
		case PickLeft:
			e.wins[p[0]]++
			e.losses[p[1]]++
		case PickRight:
			e.wins[p[1]]++
			e.losses[p[0]]++
		case PickBoth:
			e.wins[p[0]]++
			e.wins[p[1]]++
		case Skip:
			// record nothing; the pair is already marked played
		case Quit:
			e.battles-- // this battle was not judged
			e.aborted = true
			return false
		}
	}
	return true
}

// pair matches songs of similar record with the most similar available opponent,
// avoiding rematches where possible.
func (e *engine) pair(ids []int) [][2]int {
	order := append([]int(nil), ids...)
	e.rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	sort.SliceStable(order, func(i, j int) bool {
		return e.margin(order[i]) > e.margin(order[j])
	})

	used := make([]bool, len(order))
	var pairs [][2]int
	for i := range order {
		if used[i] {
			continue
		}
		a := order[i]
		best, bestDiff, bestShare, bestRematch := -1, math.MaxInt, -1, true
		for j := i + 1; j < len(order); j++ {
			if used[j] {
				continue
			}
			b := order[j]
			diff := abs(e.margin(a) - e.margin(b))
			share := sharedTags(e.tags[a], e.tags[b])
			rematch := e.played[key(a, b)]
			// Prefer: not a rematch, then closest record, then most shared tags.
			if better(rematch, diff, share, bestRematch, bestDiff, bestShare) {
				best, bestDiff, bestShare, bestRematch = j, diff, share, rematch
			}
		}
		if best == -1 {
			continue // odd one out gets a bye
		}
		b := order[best]
		used[i], used[best] = true, true
		e.played[key(a, b)] = true
		pairs = append(pairs, [2]int{a, b})
	}
	return pairs
}

// better reports whether candidate (rematch,diff,share) beats the current best under
// the ordering: fresh pairing first, then closest record, then most shared tags.
func better(rematch bool, diff, share int, bestRematch bool, bestDiff, bestShare int) bool {
	if rematch != bestRematch {
		return !rematch
	}
	if diff != bestDiff {
		return diff < bestDiff
	}
	return share > bestShare
}

// selectKeep greedily fills the set to the time budget, maximizing each song's record
// minus a redundancy penalty against songs already kept (submodular diversity).
func (e *engine) selectKeep() []int {
	return e.selectKeepWith(e.variety)
}

// selectKeepWith is selectKeep at a given diversity level; variety 0 selects purely by
// record, which finalize uses to tell "trimmed as redundant" from "just missed the cut".
func (e *engine) selectKeepWith(variety float64) []int {
	remaining := make([]int, len(e.tracks))
	for i := range remaining {
		remaining[i] = i
	}
	var kept []int
	filled := 0.0
	count := 0
	for len(remaining) > 0 {
		bi, bv := -1, math.Inf(-1)
		for _, i := range remaining {
			if v := e.valueWith(i, kept, variety); v > bv || (v == bv && (bi == -1 || i < bi)) {
				bi, bv = i, v
			}
		}
		if e.hasDur {
			if filled >= e.target {
				break
			}
			filled += e.dur[bi]
		} else {
			if float64(count) >= e.target {
				break
			}
		}
		kept = append(kept, bi)
		count++
		remaining = removeInt(remaining, bi)
	}
	return kept
}

func (e *engine) value(i int, kept []int) float64 {
	return e.valueWith(i, kept, e.variety)
}

func (e *engine) valueWith(i int, kept []int, variety float64) float64 {
	return float64(e.margin(i)) - variety*e.redundancy(i, kept)
}

// redundancy is the summed fractional tag overlap of song i with the kept set; each
// fully-overlapping kept song contributes ~1, so value falls off as a vibe saturates.
func (e *engine) redundancy(i int, kept []int) float64 {
	if len(e.tags[i]) == 0 {
		return 0
	}
	sum := 0.0
	for _, k := range kept {
		sum += float64(sharedTags(e.tags[i], e.tags[k]))
	}
	return sum / float64(len(e.tags[i]))
}

// contestedBand returns songs near the keep/cut line: a single battle could plausibly
// flip them. It is the low-value tail of the keep-set plus the high-value head of the
// cut-set, within one win of the threshold.
func (e *engine) contestedBand(kept []int) []int {
	keptSet := indexSet(kept)
	values := make([]float64, len(e.tracks))
	for i := range e.tracks {
		values[i] = e.value(i, kept)
	}
	threshold := math.Inf(1)
	for _, i := range kept {
		if values[i] < threshold {
			threshold = values[i]
		}
	}
	if math.IsInf(threshold, 1) {
		return nil
	}
	const bandWidth = 1.0
	var band []int
	for i := range e.tracks {
		if keptSet[i] {
			if values[i] <= threshold+bandWidth {
				band = append(band, i)
			}
		} else if values[i] >= threshold-bandWidth {
			band = append(band, i)
		}
	}
	return band
}

func (e *engine) keepTarget() int {
	if !e.hasDur {
		return int(e.target)
	}
	filled, count := 0.0, 0
	// Order by duration is irrelevant; approximate target count by filling shortest
	// first is unnecessary — just count how many the current selection keeps.
	for _, d := range e.dur {
		if filled >= e.target {
			break
		}
		filled += d
		count++
	}
	return count
}

func (e *engine) finalize() Result {
	kept := e.selectKeep()
	keptSet := indexSet(kept)
	marginKept := indexSet(e.selectKeepWith(0)) // what record alone would keep
	res := Result{
		Comparisons: e.battles,
		Aborted:     e.aborted,
		KeepTarget:  e.keepTarget(),
	}
	for _, i := range kept {
		res.Kept = append(res.Kept, e.tracks[i])
	}
	for i := range e.tracks {
		if keptSet[i] {
			continue
		}
		var reason CutReason
		switch {
		case e.margin(i) <= 0:
			reason = CutLost
		case marginKept[i]:
			reason = CutRedundant // record would have kept it; diversity bumped it for its vibe
		default:
			reason = CutMissed // decent record, just didn't fit the time budget
		}
		res.Cut = append(res.Cut, Cut{
			Track:      e.tracks[i],
			Reason:     reason,
			Wins:       e.wins[i],
			Losses:     e.losses[i],
			SharedKept: e.vibeNeighborsKept(i, keptSet),
		})
	}
	return res
}

// vibeNeighborsKept counts kept songs that share a majority of song i's tags — a
// graded "same vibe" measure, unlike a strict all-tags match that rarely fires.
func (e *engine) vibeNeighborsKept(i int, keptSet map[int]bool) int {
	tags := e.tags[i]
	if len(tags) == 0 {
		return 0
	}
	threshold := (len(tags) + 1) / 2
	n := 0
	for k := range keptSet {
		if sharedTags(tags, e.tags[k]) >= threshold {
			n++
		}
	}
	return n
}

func (e *engine) margin(i int) int { return e.wins[i] - e.losses[i] }

// --- helpers ---

func sharedTags(a, b []string) int {
	i, j, n := 0, 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			n++
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return n
}

func key(a, b int) [2]int {
	if a > b {
		a, b = b, a
	}
	return [2]int{a, b}
}

func indexSet(ids []int) map[int]bool {
	s := make(map[int]bool, len(ids))
	for _, i := range ids {
		s[i] = true
	}
	return s
}

func sameSet(a, b map[int]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func removeInt(s []int, v int) []int {
	for i, x := range s {
		if x == v {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
