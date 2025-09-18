package strategy_test

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YakDriver/magicmix/internal/strategy"
	"github.com/YakDriver/magicmix/internal/track"
)

const realDataFixture = "../testdata/realdata.csv"

func TestDefaultSorterRealDataEvaluation(t *testing.T) {
	t.Helper()

	allTracks := loadRealData(t)
	if len(allTracks) < 80 {
		t.Fatalf("expected at least 80 tracks in fixture, got %d", len(allTracks))
	}

	sorter := strategy.NewDefaultSorter()
	r := evaluationRNG(t)

	const rounds = 20

	totals := make([]float64, 0, rounds)
	var agg evaluationSummary

	for round := 0; round < rounds; round++ {
		sampleSize := 10 + r.Intn(71) // 10-80 tracks
		sample := randomSubset(r, allTracks, sampleSize)

		seed := r.Int63()
		ctxRound := strategy.WithSeed(context.Background(), seed)

		ordered, err := sorter.Sort(ctxRound, sample)
		if err != nil {
			t.Fatalf("sort failure round %d: %v", round, err)
		}

		score := evaluateSequence(ordered)
		totals = append(totals, score.Total)
		agg.add(score)

		t.Logf("round %02d size=%2d score=%.2f key=%.2f bpm=%.2f energy=%.2f wraps=%d jumps=%d invalid=%d",
			round+1, sampleSize, score.Total, score.KeyPenalty, score.BpmPenalty, score.EnergyPenalty,
			score.Wraps, score.BigJumpCount, score.InvalidTransitions)

		if score.InvalidTransitions > 0 {
			t.Logf("round %02d noted %d invalid/fallback transitions", round+1, score.InvalidTransitions)
		}
	}

	avg := agg.average()
	t.Logf("average score=%.2f key=%.2f bpm=%.2f energy=%.2f wraps=%d bigJumps=%d invalid=%d", avg.Total, avg.KeyPenalty, avg.BpmPenalty, avg.EnergyPenalty, avg.Wraps, avg.BigJumpCount, avg.InvalidTransitions)
}

func BenchmarkDefaultSorterRealData(b *testing.B) {
	allTracks := loadRealData(b)
	sorter := strategy.NewDefaultSorter()
	r := evaluationRNG(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sampleSize := 10 + r.Intn(71)
		sample := randomSubset(r, allTracks, sampleSize)
		seed := r.Int63()
		ctxRound := strategy.WithSeed(context.Background(), seed)

		ordered, err := sorter.Sort(ctxRound, sample)
		if err != nil {
			b.Fatalf("sort failure: %v", err)
		}
		result := evaluateSequence(ordered)
		if result.InvalidTransitions > 0 {
			b.Fatalf("invalid transition detected in benchmark run")
		}
	}
}

func loadRealData(tb testing.TB) []track.Track {
	tb.Helper()

	path := filepath.Clean(realDataFixture)
	file, err := os.Open(path)
	if err != nil {
		tb.Fatalf("real data fixture missing: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1

	var tracks []track.Track
	first := true
	line := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			tb.Fatalf("read line %d: %v", line+1, err)
		}

		line++
		if len(record) == 0 {
			continue
		}

		record = normaliseRecord(record)
		if len(record) < 5 {
			tb.Fatalf("line %d: expected at least 5 columns, got %d", line, len(record))
		}

		if first {
			first = false
			if isHeader(record) {
				continue
			}
		}

		track, err := parseTrackRecord(record)
		if err != nil {
			tb.Fatalf("line %d: %v", line, err)
		}
		tracks = append(tracks, track)
	}

	return tracks
}

func randomSubset(r *rand.Rand, all []track.Track, n int) []track.Track {
	if n >= len(all) {
		out := make([]track.Track, len(all))
		copy(out, all)
		return out
	}
	indices := r.Perm(len(all))[:n]
	sample := make([]track.Track, n)
	for i, idx := range indices {
		sample[i] = all[idx]
	}
	return sample
}

func normaliseRecord(record []string) []string {
	if len(record) <= 5 {
		return record
	}

	title := strings.Join(record[:len(record)-4], ",")
	combined := make([]string, 0, 5)
	combined = append(combined, strings.TrimSpace(title))
	combined = append(combined, record[len(record)-4:]...)
	return combined
}

func isHeader(record []string) bool {
	if len(record) < 5 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(record[0]), "title") &&
		strings.EqualFold(strings.TrimSpace(record[1]), "artist")
}

func parseTrackRecord(record []string) (track.Track, error) {
	if len(record) < 5 {
		return track.Track{}, fmt.Errorf("incomplete record: %v", record)
	}

	title := strings.TrimSpace(record[0])
	artist := strings.TrimSpace(record[1])

	bpm, err := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
	if err != nil {
		return track.Track{}, fmt.Errorf("invalid bpm: %w", err)
	}

	energy, err := strconv.Atoi(strings.TrimSpace(record[3]))
	if err != nil {
		return track.Track{}, fmt.Errorf("invalid energy: %w", err)
	}

	key, err := track.ParseKey(record[4])
	if err != nil {
		return track.Track{}, err
	}

	return track.Track{
		Title:  title,
		Artist: artist,
		BPM:    bpm,
		Energy: energy,
		Key:    key,
	}, nil
}

type evaluationScore struct {
	Total              float64
	KeyPenalty         float64
	BpmPenalty         float64
	EnergyPenalty      float64
	Wraps              int
	BigJumpCount       int
	InvalidTransitions int
}

func (e *evaluationScore) accumulate(other evaluationScore) {
	e.Total += other.Total
	e.KeyPenalty += other.KeyPenalty
	e.BpmPenalty += other.BpmPenalty
	e.EnergyPenalty += other.EnergyPenalty
	e.Wraps += other.Wraps
	e.BigJumpCount += other.BigJumpCount
	e.InvalidTransitions += other.InvalidTransitions
}

type evaluationSummary struct {
	totalRounds int
	aggregate   evaluationScore
}

func (s *evaluationSummary) add(score evaluationScore) {
	s.totalRounds++
	s.aggregate.accumulate(score)
}

func (s *evaluationSummary) average() evaluationScore {
	if s.totalRounds == 0 {
		return evaluationScore{}
	}
	n := float64(s.totalRounds)
	return evaluationScore{
		Total:              s.aggregate.Total / n,
		KeyPenalty:         s.aggregate.KeyPenalty / n,
		BpmPenalty:         s.aggregate.BpmPenalty / n,
		EnergyPenalty:      s.aggregate.EnergyPenalty / n,
		Wraps:              int(math.Round(float64(s.aggregate.Wraps) / n)),
		BigJumpCount:       int(math.Round(float64(s.aggregate.BigJumpCount) / n)),
		InvalidTransitions: int(math.Round(float64(s.aggregate.InvalidTransitions) / n)),
	}
}

func evaluateSequence(tracks []track.Track) evaluationScore {
	if len(tracks) <= 1 {
		return evaluationScore{}
	}

	score := evaluationScore{}
	sinceReset := 0

	for i := 1; i < len(tracks); i++ {
		prev := tracks[i-1]
		next := tracks[i]

		diff, wrapped := camelotDiff(prev.Key.Number, next.Key.Number)
		if wrapped {
			score.Wraps++
			sinceReset = 0
		}

		modeChange := prev.Key.Mode != next.Key.Mode

		// Key penalties
		switch {
		case diff == 0:
			score.KeyPenalty += 3
		case diff == 1:
			if modeChange {
				score.KeyPenalty += 4
				score.InvalidTransitions++
			}
		case diff == 2:
			if modeChange {
				score.KeyPenalty += 6
				score.InvalidTransitions++
			}
		case diff == 3:
			score.KeyPenalty += 4
			score.BigJumpCount++
			if modeChange {
				score.KeyPenalty += 6
				score.InvalidTransitions++
			}
		default:
			score.KeyPenalty += float64(diff * diff) // exponential penalty
			score.BigJumpCount++
			score.InvalidTransitions++
		}

		if wrapped && diff > 2 {
			score.KeyPenalty += 3
		}

		// BPM penalties
		bpmDelta := math.Abs(next.BPM - prev.BPM)
		if bpmDelta > 3 {
			score.BpmPenalty += (bpmDelta - 3) * 0.4
		}

		// Energy penalties/rewards
		energyDelta := float64(next.Energy - prev.Energy)
		if energyDelta > 14 {
			score.EnergyPenalty += (energyDelta - 14) * 0.3
		} else if energyDelta < -12 {
			// Reward strong resets after climbs.
			score.EnergyPenalty -= math.Min(4, (-energyDelta-12)*0.25)
			sinceReset = 0
		}

		sinceReset++
		if sinceReset > 12 && energyDelta >= 0 {
			score.EnergyPenalty += 0.5
		}
	}

	// Normalise to total score (lower is better).
	score.Total = score.KeyPenalty*0.6 + score.BpmPenalty*0.2 + score.EnergyPenalty*0.2
	return score
}

func camelotDiff(prev, next int) (int, bool) {
	diff := next - prev
	wrapped := false
	if diff < 0 {
		diff += 12
		wrapped = true
	}
	return diff, wrapped
}

func evaluationRNG(tb testing.TB) *rand.Rand {
	tb.Helper()
	seed := time.Now().UnixNano()
	if override := os.Getenv("MAGICMIX_EVAL_SEED"); override != "" {
		if parsed, err := strconv.ParseInt(override, 10, 64); err == nil {
			seed = parsed
		} else {
			tb.Logf("invalid MAGICMIX_EVAL_SEED %q, using time-based seed", override)
		}
	}
	tb.Logf("evaluation seed=%d", seed)
	return rand.New(rand.NewSource(seed))
}
