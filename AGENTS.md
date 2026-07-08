# Repository Guidelines

## What magicmix does
Given a CSV of tracks, magicmix orders them into a smooth, intentional set and drops
songs that don't fit. Ordering quality is defined by **one** scoring model in
`internal/strategy/score.go`; the `flow` strategy minimizes exactly that score, so
"what we optimize" and "what `--score` reports" never drift apart. Don't add
per-strategy scorers — change scoring in `score.go`.

## Scoring model (source of truth)
Lower is better; signals absent from the data are skipped.

- **Coherence** (pairwise, adjacent songs): harmonic Camelot compatibility +
  octave-folded tempo, always on; valence (mood) and acousticness continuity when the
  data has them.
- **Contour** (global energy shape): intensity (energy blended with danceability)
  should move in *waves* of ~18–30 min of playtime (falls back to a 6–10 track cadence
  when `length` is absent). A **reset** — a drop that starts a new build — is free after
  a real build; **jitter** (choppy oscillation) and **monotony** (one long ramp) are
  penalized. The ending is **neutral**: don't reward "finish strong," which needs
  payoff the data can't see.

## Layout
- `cmd/magicmix` — CLI entrypoint.
- `internal/cli` — flags and wiring (`--strategy`, `--seed`, `--limit`, `--keep-all`, `--score`).
- `internal/strategy` — strategies (`flow` is primary; `chave` groups songs into
  themed ~20-30 min chapters; `default`/`eloise`/`constance` are legacy), the scoring
  model (`score.go`), tag extraction (`tags.go`), and outlier detection (`outliers.go`).
- `internal/tournament` — the interactive keep/cut selector behind `magicmix
  tournament`: Swiss-style pairwise auditions with redundancy-penalized selection to a
  time budget. Pure engine (driven by a `Judge`); the keypress UI lives in
  `internal/cli/tournament.go`.
- `internal/track`, `internal/csvio` — domain model and header-aware CSV IO.
- `internal/testdata` — fixtures.

## Build, test, develop
- `make build` / `make test` / `make ci` (build + test + vet + modernize) / `make lint` (golangci-lint).
- Ad-hoc run: `go run ./cmd/magicmix --input input.csv --strategy flow --seed 9876`.
- Sandbox-only: if `$HOME` caches aren't writable, prefix commands with
  `GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache` and `rm -rf` them after. Not
  needed for normal local development.

## Conventions
- Modern idiomatic Go; gofmt; keep `make lint` clean (no dead code, no unchecked errors).
- Tests sit beside code as `*_test.go`; prefer deterministic seeds (`strategy.WithSeed`).
- Optional track signals are pointer fields (`*int`): `nil` means "absent." Scoring must
  skip absent signals, never assume a value.

## Notes
- Determinism: same `--seed` + same input → same output. (Per-seed variety is a planned
  enhancement.)
- Outliers: by default up to ~10% of poorly-fitting tracks are dropped and reported;
  `--keep-all` forces everything in.
