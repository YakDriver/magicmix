# Repository Guidelines

## Purpose
Given an input list of tracks, magicmix creates an optimal output list. What "optimal" means depends on the stategy. "Optimal" relates to the ordering of output tracks, where there is a transition from a track to the next in the list. Transitions are described with N#L#, where N and L relate to Camelot keys, N=number (1-12) and L=letter (A or B). For example, 1A to 1A is a N0L0 transition; 3A to 4B is N1L1, 5B to 3A is N-2L1. The keys wrap so 12B to 1B is N1L0 and 11A to 1A is N2L0.

For the default strategy, the optimal ordering preserves:
1. Mixing in key:
    - Good: N0L0, N1L0, N2L0, N0L1
    - Okay: N3L0, N4L0, N-1L0
    - Bad: all other transitions are bad
2. Incremental BPM changes: BPM should be optimized to change as little as possible from track to track givens the limitations of the set of BPMs of the tracks.
3. Energy increase: Energy should increase from track to track except periodically dropping to then start increasing again.
4. Too many of one transition in a row, even if good, become bad (e.g., 5 N1L0s in a row).
5. For each run, magicmix should produce a different random optimal ordering of the input tracks.

## Project Structure & Module Organization
- `cmd/magicmix`: CLI entrypoint (`main.go`) wiring to the CLI package.
- `internal/cli`: flag parsing, context wiring (`--limit`, `--seed`, `--timeout`).
- `internal/strategy`: sorting strategies, planner, evaluation harness, and tests.
- `internal/track`, `internal/csvio`: domain models and CSV IO helpers.
- `internal/testdata`: shared fixtures (e.g., `realdata.csv`) for experimentation.

## Build, Test, and Development Commands
- `GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go build ./...`: compile with local caches to avoid sandbox issues.
- `GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go test ./...`: run the unit suite.
- Caches: `.gocache` and `.gomodcache` are intentionally local—clean them (`rm -rf`) after build/test runs.
- `MAGICMIX_EVAL_SEED=12345 go test -run TestDefaultSorterRealDataEvaluation ./internal/strategy`: execute the 20-round real-data evaluation with a repeatable seed.
- `go run ./cmd/magicmix --input input.csv --limit 20 --seed 9876`: ad-hoc CLI run with truncation and deterministic randomness.

## Coding Style & Naming Conventions
- Follow modern idiomatic Go
- gofmt

## Testing Guidelines
- Unit tests use Go’s `testing` package; place files as `*_test.go` next to the code (`internal/cli/cli_test.go`).
- Prefer deterministic seeds for assertions (see `strategy.WithSeed`); random seeds only for exploratory runs.
- Evaluation harness lives in `internal/strategy/eval_test.go`; repeat runs with `MAGICMIX_EVAL_SEED` when comparing changes.

## Additional Notes
- Randomness: the default strategy reads `--seed`
