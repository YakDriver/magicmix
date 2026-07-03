package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/YakDriver/magicmix/internal/csvio"
	"github.com/YakDriver/magicmix/internal/tournament"
	"github.com/YakDriver/magicmix/internal/track"
)

const defaultVariety = 0.6

// runTournament handles `magicmix tournament ...`: an interactive, keypress-driven
// audition that selects which songs to keep for a set of a given length, then writes
// the keep-set CSV. It does not order the result — feed the output into `--strategy
// flow` or `chave`.
func runTournament(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("magicmix tournament", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	inputPath := fs.String("input", "", "Path to the input CSV file")
	outputPath := fs.String("output", "", "Path to write the keep-set CSV")
	minutes := fs.Float64("time", 0, "Target set length in minutes (e.g. 180 for 3 hours)")
	variety := fs.Float64("variety", defaultVariety, "Diversity: how hard over-represented vibes are pared down")
	seedFlag := fs.Int64("seed", 0, "Optional seed for deterministic pairing")

	fs.Usage = func() {
		w := fs.Output()
		_, _ = fmt.Fprintf(w, "Usage: magicmix tournament --input FILE --time MINUTES [options]\n\n")
		_, _ = fmt.Fprintf(w, "Pick between two songs at a time; magicmix keeps the ones that fit a set of\n")
		_, _ = fmt.Fprintf(w, "--time minutes, paring down over-represented vibes. Writes a keep-set CSV to\n")
		_, _ = fmt.Fprintf(w, "feed into `--strategy flow` or `chave`.\n\nOptions:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *inputPath == "" {
		fs.Usage()
		return errors.New("input path is required")
	}
	if *minutes <= 0 {
		fs.Usage()
		return errors.New("--time (minutes) is required and must be positive")
	}

	tracks, err := csvio.Load(ctx, *inputPath)
	if err != nil {
		return err
	}
	if len(tracks) < 2 {
		return fmt.Errorf("need at least 2 tracks to run a tournament, got %d", len(tracks))
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("tournament needs an interactive terminal (stdin is not a TTY)")
	}

	fmt.Printf("Tournament: %d songs -> a ~%.0f min set\n", len(tracks), *minutes)
	fmt.Printf("Diversity: %.2f (raise with --variety to cut over-represented vibes harder)\n", *variety)
	fmt.Print("Meta: key · bpm · length · year · nrg/dnc/val/aco/pop (0-100)\n\n")
	fmt.Print("  [1]/[2] keep that song   ·   3 both great   ·   0 neither   ·   s skip   ·   q finish\n\n")

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("enter raw terminal mode: %w", err)
	}
	judge := newKeypressJudge()
	res, runErr := tournament.Run(ctx, tracks, judge, tournament.Config{
		TargetMinutes: *minutes,
		Variety:       *variety,
		Seed:          *seedFlag,
	})
	_ = term.Restore(fd, oldState)
	fmt.Println()
	if runErr != nil {
		return runErr
	}

	resolvedOutput := *outputPath
	if resolvedOutput == "" {
		resolvedOutput = deriveTournamentOutput(*inputPath)
	}
	if err := csvio.Save(ctx, resolvedOutput, res.Kept); err != nil {
		return err
	}

	printTournamentSummary(res)
	fmt.Printf("\nWrote %d kept tracks to %s\n", len(res.Kept), resolvedOutput)
	fmt.Printf("Order them next: magicmix --input %s --strategy flow\n", resolvedOutput)
	return nil
}

// newKeypressJudge renders each matchup and blocks on a single keystroke.
func newKeypressJudge() tournament.Judge {
	var buf [1]byte
	return func(m tournament.Matchup) tournament.Outcome {
		renderMatchup(m)
		for {
			n, err := os.Stdin.Read(buf[:])
			if err != nil || n == 0 {
				return tournament.Quit
			}
			if out, ok := decodeKey(buf[0]); ok {
				return out
			}
		}
	}
}

// decodeKey maps a keystroke to an outcome; the bool is false for keys to ignore.
func decodeKey(b byte) (tournament.Outcome, bool) {
	switch b {
	case '1':
		return tournament.PickLeft, true
	case '2':
		return tournament.PickRight, true
	case '3', 'b', 'B':
		return tournament.PickBoth, true
	case '0', 'n', 'N':
		return tournament.PickNeither, true
	case 's', 'S':
		return tournament.Skip, true
	case 'q', 'Q', 3: // 3 = Ctrl-C
		return tournament.Quit, true
	}
	return 0, false
}

func renderMatchup(m tournament.Matchup) {
	fmt.Printf("\r\n%s · Battle %d · ~%d kept · %d still contested\r\n\r\n",
		m.Phase, m.Battle, m.KeepEstimate, m.Contested)
	fmt.Printf("  [1]  %s\r\n       %s\r\n\r\n", songTitle(m.Left), songMeta(m.Left))
	fmt.Printf("  [2]  %s\r\n       %s\r\n\r\n", songTitle(m.Right), songMeta(m.Right))
	fmt.Print("  1/2 keep · 3 both · 0 neither · s skip · q finish\r\n")
}

func songTitle(t track.Track) string {
	return fmt.Sprintf("%s — %s", t.Title, t.Artist)
}

func songMeta(t track.Track) string {
	head := []string{t.Key.String(), fmt.Sprintf("%.0fbpm", t.BPM)}
	if t.Duration != nil {
		head = append(head, fmt.Sprintf("%d:%02d", *t.Duration/60, *t.Duration%60))
	}
	if t.Year != nil {
		head = append(head, fmt.Sprintf("%d", *t.Year))
	}

	sig := []string{fmt.Sprintf("nrg%d", t.Energy)}
	sig = appendSig(sig, "dnc", t.Danceability)
	sig = appendSig(sig, "val", t.Valence)
	sig = appendSig(sig, "aco", t.Acousticness)
	sig = appendSig(sig, "pop", t.Popularity)

	return strings.Join(head, " · ") + " · " + strings.Join(sig, " ")
}

func appendSig(sig []string, abbr string, v *int) []string {
	if v != nil {
		sig = append(sig, fmt.Sprintf("%s%d", abbr, *v))
	}
	return sig
}

func printTournamentSummary(res tournament.Result) {
	if res.Aborted {
		fmt.Printf("Finished early after %d comparisons — keeping the current best guess.\n", res.Comparisons)
	} else {
		fmt.Printf("Done in %d comparisons.\n", res.Comparisons)
	}

	var lost, missed, redundant int
	for _, c := range res.Cut {
		switch c.Reason {
		case tournament.CutLost:
			lost++
		case tournament.CutMissed:
			missed++
		case tournament.CutRedundant:
			redundant++
		}
	}
	fmt.Printf("Kept %d · cut %d (%d lost, %d just missed the cut, %d trimmed as redundant)\n",
		len(res.Kept), len(res.Cut), lost, missed, redundant)

	shown := 0
	for _, c := range res.Cut {
		if c.Reason != tournament.CutRedundant {
			continue
		}
		if shown == 0 {
			fmt.Println("\nTrimmed as redundant (a winning record, but its vibe was already covered):")
		}
		if shown >= 10 {
			fmt.Printf("  ... and %d more\n", redundant-shown)
			break
		}
		fmt.Printf("  - %s — %s (%d-%d, %d kept song(s) share its vibe)\n",
			c.Track.Title, c.Track.Artist, c.Wins, c.Losses, c.SharedKept)
		shown++
	}
}

func deriveTournamentOutput(input string) string {
	dir := filepath.Dir(input)
	base := filepath.Base(input)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = ".csv"
	}
	return filepath.Join(dir, fmt.Sprintf("%s_keep%s", name, ext))
}
