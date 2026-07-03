# magicmix

`magicmix` sequences a tracklist into a smooth, intentional set: it orders songs so
key, tempo, mood, and energy flow well — and drops the few that don't fit.

## Quick start

```bash
# order a playlist (writes tracks_magicmix.csv next to the input)
go run ./cmd/magicmix --input tracks.csv --strategy flow

# score an existing ordering instead of sorting
go run ./cmd/magicmix --input tracks.csv --score          # summary
go run ./cmd/magicmix --input tracks.csv --score-verbose  # full breakdown

# pick which songs make the cut for a set of a given length (interactive)
go run ./cmd/magicmix tournament --input tracks.csv --time 180
```

The run prints the seed it used (rerun with `--seed=<value>`) and lists any tracks it
dropped.

## Tournament: choosing what to keep

`tournament` is an interactive culler for when you have more songs than set. It shows
two songs at a time; you press **1** or **2** to keep the one you'd rather hear (**s**
skip, **q** finish). magicmix keeps the songs that fill a set of `--time` minutes and
cuts the rest, then writes a `<input>_keep.csv` you can feed into `flow` or `chave`.

```bash
go run ./cmd/magicmix tournament --input tracks.csv --time 180   # ~3-hour set
go run ./cmd/magicmix --input tracks_keep.csv --strategy chave   # then order it
```

It's built to ask as few questions as possible: it only needs a keep/cut decision, not
a ranking or a winner, so battles concentrate on the songs near the cut line. Matchups
are fair (like-vs-like — songs that share a vibe). A **diversity** knob (shown at
start, tune with `--variety`) pares down over-represented vibes: raise it to keep the
best few of a common sound and make room for rarer ones. At the end it reports what it
cut and why (lost its auditions vs. trimmed as redundant).

## Input CSV

A header row is matched by name — case-insensitive, order and extra columns don't
matter:

- **Required:** `title`, `artist`, `bpm`, `energy`, `key` (Camelot, e.g. `8B`)
- **Optional, used when present:** `danceability`, `valence`, `popularity`,
  `acousticness`, `length` (`m:ss`), `release` (a date or year, e.g. `2024-05-01`)

Headerless files fall back to positional `title,artist,bpm,energy,key`.

## Strategies

- **`flow`** (recommended) — treats ordering as a path-optimization problem and
  minimizes the exact score `--score` reports.
- **`chave`** — builds the set from *chaves* (themed ~20-30 min chapters): each groups
  songs that share three traits (e.g. modern + danceable + popular) and builds in
  intensity. Trades some transition smoothness for human-noticeable grouping.
- `default`, `eloise`, `constance` — earlier heuristics kept for comparison.

List them with `--list-strategies`.

## How it scores (lower is better)

One adaptive model — signals you don't have are skipped:

- **Coherence** — each song vs. the next: harmonic Camelot fit + tempo
  (octave-folded, so 90↔180 BPM counts as close) + valence and acousticness when
  available.
- **Contour** — the whole set's energy shape: it should build in waves of ~20 minutes.
  A *reset* (a deliberate drop that starts a new build) is free; jitter and one long
  ramp are penalized. The ending is neutral.

## Options

| Flag | Purpose |
| --- | --- |
| `--input` | source CSV (required) |
| `--output` | destination (default `<input>_magicmix.csv`) |
| `--strategy` | ordering strategy — `flow` (smoothest) or `chave` (themed chapters) |
| `--keep-all` | keep every track (by default up to 10% of misfits are dropped and reported) |
| `--limit` | cap how many tracks are written |
| `--seed` | deterministic seed (`0`/omitted = time-based; printed so you can rerun) |
| `--timeout` | processing timeout, e.g. `30s` |
| `--score`, `--score-verbose` | score the input instead of sorting |
| `--list-strategies` | print strategies and exit |

## Develop

```bash
make build   # compile
make install # install the magicmix binary to $GOBIN
make test    # unit tests
make ci      # build + test + vet + modernize
make lint    # golangci-lint
```
