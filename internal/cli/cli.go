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
