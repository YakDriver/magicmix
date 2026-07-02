package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YakDriver/magicmix/internal/csvio"
	"github.com/YakDriver/magicmix/internal/strategy"
)

// Run is the entry point for the CLI application.
func Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	return run(ctx, os.Args[1:])
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("magicmix", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	inputPath := fs.String("input", "", "Path to the input CSV file")
	outputPath := fs.String("output", "", "Path to write the sorted CSV file")
	strategyName := fs.String("strategy", "default", "Sorting strategy to apply")
	listStrategies := fs.Bool("list-strategies", false, "List available strategies and exit")
	limit := fs.Int("limit", 0, "Optional maximum number of tracks to write")
	seedFlag := fs.Int64("seed", 0, "Optional seed for pseudo-random decisions (defaults to time-based)")
	timeout := fs.Duration("timeout", 0, "Optional timeout for processing (e.g. 30s)")
	scoreOnly := fs.Bool("score", false, "Score the transitions in the input file (no sorting)")
	scoreVerbose := fs.Bool("score-verbose", false, "Include detailed scoring breakdown (implies --score)")

	fs.Usage = func() {
		w := fs.Output()
		_, _ = fmt.Fprintf(w, "Usage: %s [options]\n\nOptions:\n", fs.Name())
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(w, "\nAvailable strategies: %s\n", strings.Join(strategy.Names(), ", "))
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *listStrategies {
		for _, name := range strategy.Names() {
			fmt.Println(name)
		}
		return nil
	}

	if *inputPath == "" {
		fs.Usage()
		return errors.New("input path is required")
	}

	if *limit < 0 {
		return errors.New("limit must be non-negative")
	}

	// Handle scoring mode
	isScoring := *scoreOnly || *scoreVerbose
	if isScoring {
		return runScoring(*inputPath, *scoreVerbose)
	}

	ctx, cancel := maybeWithTimeout(ctx, *timeout)
	if cancel != nil {
		defer cancel()
	}

	if *limit > 0 {
		ctx = strategy.WithLimit(ctx, *limit)
	}

	effectiveSeed := *seedFlag
	if effectiveSeed == 0 {
		effectiveSeed = time.Now().UnixNano()
	}
	ctx = strategy.WithSeed(ctx, effectiveSeed)

	sorter, err := strategy.Get(*strategyName)
	if err != nil {
		return err
	}

	tracks, err := csvio.Load(ctx, *inputPath)
	if err != nil {
		return err
	}

	result, err := strategy.Sort(ctx, sorter, tracks)
	if err != nil {
		return err
	}

	fmt.Printf("Using seed %d\n", effectiveSeed)

	resolvedOutput := *outputPath
	if resolvedOutput == "" {
		resolvedOutput = deriveOutputPath(*inputPath)
	}

	ordered := result.Ordered
	if *limit > 0 && *limit < len(ordered) {
		ordered = ordered[:*limit]
		fmt.Printf("Applying limit %d; writing first %d tracks\n", *limit, len(ordered))
	}

	if err := csvio.Save(ctx, resolvedOutput, ordered); err != nil {
		return err
	}

	fmt.Printf("Wrote %d tracks using %s strategy to %s\n", len(ordered), sorter.Name(), resolvedOutput)
	return nil
}

func maybeWithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, nil
	}
	return context.WithTimeout(ctx, timeout)
}

func runScoring(inputPath string, verbose bool) error {
	ctx := context.Background()
	tracks, err := csvio.Load(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("failed to read tracks from %s: %w", inputPath, err)
	}

	if len(tracks) <= 1 {
		fmt.Printf("File %s contains %d track(s) - no transitions to score\n", inputPath, len(tracks))
		return nil
	}

	score := strategy.ScoreMix(tracks)

	fmt.Printf("=== MIX QUALITY SCORING for %s ===\n", inputPath)
	fmt.Printf("Total: %.2f (0 = perfect) | per track: %.3f | transitions: %d\n",
		score.Total, score.PerTrack, score.Transitions)
	fmt.Printf("Active signals: %s\n", strings.Join(score.ActiveSignals, ", "))

	fmt.Printf("\nCoherence (adjacent-song fit):\n")
	fmt.Printf("  Harmonic (key): %8.2f\n", score.HarmonicTotal)
	fmt.Printf("  Tempo (BPM):    %8.2f\n", score.TempoTotal)
	if score.ValenceTotal > 0 {
		fmt.Printf("  Valence (mood): %8.2f\n", score.ValenceTotal)
	}
	if score.AcousticTotal > 0 {
		fmt.Printf("  Acousticness:   %8.2f\n", score.AcousticTotal)
	}

	c := score.Contour
	fmt.Printf("\nContour (energy shape): %8.2f\n", score.ContourTotal)
	fmt.Printf("  Waves: %d | resets: %d (target %d-%d)\n", c.Builds, c.Resets, c.TargetResetLo, c.TargetResetHi)
	fmt.Printf("  Build smoothness: %.2f | jitter resets: %.2f | wave-count: %.2f\n",
		c.SmoothnessPenalty, c.ResetPenalty, c.WaveCountPenalty)

	if len(score.Worst) > 0 {
		limit := 5
		if verbose {
			limit = len(score.Worst)
		}
		fmt.Printf("\nRoughest adjacent transitions:\n")
		for i, d := range score.Worst {
			if i >= limit || d.Pairwise <= 0 {
				break
			}
			fmt.Printf("  #%d %s (%s) -> %s (%s): %.2f [key %.2f tempo %.2f mood %.2f acoustic %.2f]\n",
				d.Index+1, truncate(d.FromTitle, 24), d.FromKey, truncate(d.ToTitle, 24), d.ToKey,
				d.Pairwise, d.Harmonic, d.Tempo, d.Valence, d.Acoustic)
		}
	}

	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func deriveOutputPath(input string) string {
	dir := filepath.Dir(input)
	base := filepath.Base(input)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = ".csv"
	}
	outputName := fmt.Sprintf("%s_magicmix%s", name, ext)
	return filepath.Join(dir, outputName)
}
