# magicmix

`magicmix` sequences a tracklist into a smooth, intentional set: it orders songs so
key, tempo, mood, and energy flow well — and drops the few that don't fit.

## Install

```bash
go install github.com/YakDriver/magicmix/cmd/magicmix@latest
```

This puts a `magicmix` binary in `$(go env GOPATH)/bin` (or `$GOBIN`) — make sure
that's on your `PATH`. To run from a checkout instead, see [Develop](#develop).

## Quick start

```bash
# order a playlist (writes tracks_magicmix.csv next to the input)
magicmix --input tracks.csv --strategy flow

# score an existing ordering instead of sorting
magicmix --input tracks.csv --score          # summary
magicmix --input tracks.csv --score-verbose  # full breakdown

# pick which songs make the cut for a set of a given length (interactive)
magicmix tournament --input tracks.csv --time 180
```

The run prints the seed it used (rerun with `--seed=<value>`) and lists any tracks it
dropped.

## Tournament: choosing what to keep

`tournament` is an interactive culler for when you have more songs than set. It shows
two songs at a time; you press **1** or **2** to keep the one you'd rather hear, **3**
when both are great (boosts both), **0** when neither grabs you (nudges both toward the
cut), **s** to skip, **q** to finish. magicmix keeps the songs that fill a set of
`--time` minutes and cuts the rest, then writes a `<input>_keep.csv` you can feed into
`flow` or `chave`.

```bash
magicmix tournament --input tracks.csv --time 180   # ~3-hour set
magicmix --input tracks_keep.csv --strategy chave   # then order it
```

It's built to ask as few questions as possible: it only needs a keep/cut decision, not
a ranking or a winner, so battles concentrate on the songs near the cut line. Matchups
are fair (like-vs-like — songs that share a vibe). A **diversity** knob (shown at
start, tune with `--variety`) pares down over-represented vibes: raise it to keep the
best few of a common sound and make room for rarer ones. At the end it reports what it
cut and why: **lost** (more losses than wins), **just missed the cut** (a fine record
that didn't fit the time), or **trimmed as redundant** (a winning record bumped because
its vibe was already covered).

Tournament flags:

| Flag | Purpose |
| --- | --- |
| `--input` | source CSV (required) |
| `--time` | target set length in minutes, e.g. `180` (required) |
| `--variety` | diversity knob (default `0.6`); higher pares over-represented vibes harder |
| `--output` | keep-set destination (default `<input>_keep.csv`) |
| `--seed` | deterministic pairing (`0`/omitted = time-based) |

It needs an interactive terminal (it reads single keypresses).

## Input CSV

A header row is matched by name — case-insensitive, order and extra columns don't
matter:

- **Required:** `title`, `artist`, `bpm`, `energy`, `key` (Camelot, e.g. `8B`)
- **Optional, used when present:** `danceability`, `valence`, `popularity`,
  `acousticness`, `length` (`m:ss`), `release` (a date or year, e.g. `2024-05-01`)

Headerless files fall back to positional `title,artist,bpm,energy,key`.

Output is a faithful pass-through: the written CSV keeps the input's columns in the
same order — including extra columns magicmix doesn't use — with only the rows
reordered (and dropped tracks omitted). Input line endings are preserved. Values
aren't rewritten, so an unused index column like `#` stays as-is and will read out of
sequence after reordering.

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

These apply to the ordering/scoring command (`--strategy`/`--score`); the `tournament`
subcommand has its own flags (see above).

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

Run from a checkout without installing:

```bash
go run ./cmd/magicmix --input tracks.csv --strategy flow
```
