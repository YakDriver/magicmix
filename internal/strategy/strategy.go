package strategy

import (
	"context"

	"github.com/YakDriver/magicmix/internal/track"
)

// Sorter arranges tracks in an order tailored to a specific optimization strategy.
type Sorter interface {
	Name() string
	Sort(ctx context.Context, tracks []track.Track) ([]track.Track, error)
}

// Result captures the ordered output and any metadata about the sort run.
type Result struct {
	Ordered []track.Track
	Notes   []string
}

// Sort applies the sorter and wraps the result in a Result struct for future expansion.
func Sort(ctx context.Context, s Sorter, tracks []track.Track) (Result, error) {
	ordered, err := s.Sort(ctx, tracks)
	if err != nil {
		return Result{}, err
	}
	return Result{Ordered: ordered}, nil
}

type contextKey string

const limitContextKey contextKey = "strategy.limit"
const seedContextKey contextKey = "strategy.seed"

// WithLimit annotates the context with a maximum track count that Sorters can honour.
func WithLimit(ctx context.Context, limit int) context.Context {
	if limit <= 0 {
		return ctx
	}
	return context.WithValue(ctx, limitContextKey, limit)
}

func limitFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if limit, ok := ctx.Value(limitContextKey).(int); ok {
		return limit
	}
	return 0
}

// WithSeed stores a deterministic seed for strategy randomness.
func WithSeed(ctx context.Context, seed int64) context.Context {
	return context.WithValue(ctx, seedContextKey, seed)
}

func seedFromContext(ctx context.Context) (int64, bool) {
	if ctx == nil {
		return 0, false
	}
	val := ctx.Value(seedContextKey)
	if seed, ok := val.(int64); ok {
		return seed, true
	}
	return 0, false
}
