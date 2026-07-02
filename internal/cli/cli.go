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
	scoreOnly := fs.Bool("score", false, "Score the key transitions in the input file (no sorting)")
	scoreVerbose := fs.Bool("score-verbose", false, "Include detailed scoring breakdown (implies --score)")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [options]\n", fs.Name())
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "Options:")
		fs.PrintDefaults()
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintf(fs.Output(), "Available strategies: %s\n", strings.Join(strategy.Names(), ", "))
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

	mixScore := strategy.ScoreMix(tracks)
	keyScore := mixScore.KeyScore

	// Always show summary
	fmt.Printf("=== MIX QUALITY SCORING for %s ===\n", inputPath)
	fmt.Printf("Total Score: %d (0 = perfect)\n", mixScore.Total)
	fmt.Printf("\nKey Transition Score: %d\n", keyScore.Total)
	fmt.Printf("  Mode & Number Changes: %d points\n", keyScore.ModeAndNumberPts)
	fmt.Printf("  Number Change Penalties: %d points\n", keyScore.NumberChangePts)
	fmt.Printf("  Run Penalties: %d points\n", keyScore.RunPenaltyPts)

	// Show BPM scoring if available
	if mixScore.BPMScore != nil {
		bpmScore := *mixScore.BPMScore
		fmt.Printf("\nBPM Transition Score: %d\n", bpmScore.Total)
		fmt.Printf("  BPM Change Penalties: %d points\n", bpmScore.TransitionPts)
		fmt.Printf("  Cross-Genre Penalties: %d points\n", bpmScore.RangePenaltyPts)
		fmt.Printf("  Tempo Shock Penalties: %d points\n", bpmScore.TempoShockPts)
	}

	// Show Energy scoring if available
	if mixScore.EnergyScore != nil {
		energyScore := *mixScore.EnergyScore
		fmt.Printf("\nEnergy Flow Score: %d\n", energyScore.Total)
		fmt.Printf("  Energy Flow Penalties: %d points\n", energyScore.EnergyFlowPts)
		fmt.Printf("  Key Progression Penalties: %d points\n", energyScore.KeyProgressionPts)
		fmt.Printf("  Energy Drop Penalties: %d points\n", energyScore.EnergyDropPts)
		fmt.Printf("  Plateau Penalties: %d points\n", energyScore.PlateauPts)
	}

	fmt.Printf("Total tracks: %d\n", len(tracks))

	if mixScore.Total > 0 {
		fmt.Printf("Average penalty per track: %.2f\n", float64(mixScore.Total)/float64(len(tracks)))
	}

	// Show detailed breakdown if verbose
	if verbose && len(keyScore.TransitionDetails) > 0 {
		fmt.Printf("\nDetailed Key Transitions:\n")
		for i, detail := range keyScore.TransitionDetails {
			totalPts := detail.ModeAndNumberPts + detail.NumberChangePts + detail.RunPenaltyPts
			if totalPts > 0 {
				fmt.Printf("  Track %d->%d: %s->%s (%d pts) - %s",
					i+1, i+2, detail.FromKey, detail.ToKey, totalPts, detail.Description)
				if detail.RunLength > 0 {
					fmt.Printf(" [run of %d]", detail.RunLength)
				}
				fmt.Println()
			}
		}

		// Show BPM details if available
		if mixScore.BPMScore != nil && len(mixScore.BPMScore.TransitionDetails) > 0 {
			fmt.Printf("\nDetailed BPM Transitions:\n")
			for i, detail := range mixScore.BPMScore.TransitionDetails {
				totalPts := detail.TransitionPts + detail.RangePenaltyPts + detail.TempoShockPts
				if totalPts > 0 {
					fmt.Printf("  Track %d->%d: %.1f->%.1f BPM (%d pts) - %s\n",
						i+1, i+2, detail.FromBPM, detail.ToBPM, totalPts, detail.Description)
				}
			}
		}

		// Show Energy details if available
		if mixScore.EnergyScore != nil && len(mixScore.EnergyScore.TransitionDetails) > 0 {
			fmt.Printf("\nDetailed Energy Transitions:\n")
			for i, detail := range mixScore.EnergyScore.TransitionDetails {
				totalPts := detail.EnergyFlowPts + detail.KeyProgressionPts + detail.EnergyDropPts
				if totalPts > 0 {
					fmt.Printf("  Track %d->%d: %d->%d energy (%d pts) - %s\n",
						i+1, i+2, detail.FromEnergy, detail.ToEnergy, totalPts, detail.Description)
				}
			}
		}
	} else if mixScore.Total > 0 {
		// Show just the worst offenders in non-verbose mode
		fmt.Printf("\nWorst transitions (penalty >= 2):\n")

		// Key transitions
		count := 0
		for i, detail := range keyScore.TransitionDetails {
			totalPts := detail.ModeAndNumberPts + detail.NumberChangePts + detail.RunPenaltyPts
			if totalPts >= 2 {
				fmt.Printf("  Track %d->%d: %s->%s (%d pts) - %s",
					i+1, i+2, detail.FromKey, detail.ToKey, totalPts, detail.Description)
				if detail.RunLength > 0 {
					fmt.Printf(" [run of %d]", detail.RunLength)
				}
				fmt.Println()
				count++
				if count >= 5 { // Limit key transitions to 5 to make room for BPM/Energy
					break
				}
			}
		}

		// BPM transitions (worst few)
		if mixScore.BPMScore != nil {
			bpmCount := 0
			for i, detail := range mixScore.BPMScore.TransitionDetails {
				totalPts := detail.TransitionPts + detail.RangePenaltyPts + detail.TempoShockPts
				if totalPts >= 2 && bpmCount < 3 {
					fmt.Printf("  Track %d->%d: %.1f->%.1f BPM (%d pts) - %s\n",
						i+1, i+2, detail.FromBPM, detail.ToBPM, totalPts, detail.Description)
					bpmCount++
				}
			}
		}

		// Energy transitions (worst few)
		if mixScore.EnergyScore != nil {
			energyCount := 0
			for i, detail := range mixScore.EnergyScore.TransitionDetails {
				totalPts := detail.EnergyFlowPts + detail.KeyProgressionPts + detail.EnergyDropPts
				if totalPts >= 2 && energyCount < 3 {
					fmt.Printf("  Track %d->%d: %d->%d energy (%d pts) - %s\n",
						i+1, i+2, detail.FromEnergy, detail.ToEnergy, totalPts, detail.Description)
					energyCount++
				}
			}
		}

		if count >= 5 {
			remaining := countTransitionsWithScore(keyScore.TransitionDetails, 2) - count
			if remaining > 0 {
				fmt.Printf("  ... (%d more transitions not shown, use --score-verbose for full details)\n", remaining)
			}
		}
	}

	return nil
}

func countTransitionsWithScore(details []strategy.KeyTransitionDetail, minScore int) int {
	count := 0
	for _, detail := range details {
		if detail.ModeAndNumberPts+detail.NumberChangePts+detail.RunPenaltyPts >= minScore {
			count++
		}
	}
	return count
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
