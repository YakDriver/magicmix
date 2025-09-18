# magicmix

`magicmix` is an experimental Go CLI for sequencing DJ tracklists. The initial release focuses on providing a flexible framework for importing tracks, applying pluggable ordering strategies, and exporting the result.

## Status

- CSV input with the columns `title,artist,bpm,energy,key`
- Output written as a CSV next to the input file (use `--output` to override)
- Default sorting strategy that balances Camelot key flow, incremental BPM adjustments, and cyclical energy ramps using heuristics
- Strategy registry ready for future heuristic optimizations

## Usage

```bash
# build the CLI
GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go build ./...

# run against a csv file
./magicmix --input tracks.csv --output ordered.csv

# inspect available strategies
./magicmix --list-strategies

# run 20-round evaluation against the real-data fixture with a repeatable seed
MAGICMIX_EVAL_SEED=12345 go test -run TestDefaultSorterRealDataEvaluation ./internal/strategy

# run tests
GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go test ./...
```

Options:

- `--input` (required) – path to the source CSV
- `--output` – destination file; defaults to `<input>_magicmix.csv`
- `--strategy` – sorting strategy name (default `default`)
- `--list-strategies` – print registered strategies and exit
- `--timeout` – optional processing timeout (e.g. `30s`)
- `--limit` – maximum number of tracks to output (leave unset to keep all)
- `--seed` – seed for pseudo-random decisions (omit for a different shuffle each run)

## Next Steps

- Tune heuristic weights with sample tracklists and automated evaluation
- Add more strategies and comparison tooling
- Expand input formats
