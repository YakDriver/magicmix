package cli

import (
	"testing"

	"github.com/YakDriver/magicmix/internal/tournament"
)

func TestDecodeKey(t *testing.T) {
	cases := []struct {
		key  byte
		want tournament.Outcome
		ok   bool
	}{
		{'1', tournament.PickLeft, true},
		{'2', tournament.PickRight, true},
		{'s', tournament.Skip, true},
		{'S', tournament.Skip, true},
		{'q', tournament.Quit, true},
		{3, tournament.Quit, true}, // Ctrl-C
		{'x', 0, false},
		{'\n', 0, false},
	}
	for _, c := range cases {
		got, ok := decodeKey(c.key)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("decodeKey(%q) = (%v,%v), want (%v,%v)", c.key, got, ok, c.want, c.ok)
		}
	}
}

func TestDeriveTournamentOutput(t *testing.T) {
	got := deriveTournamentOutput("/music/party.csv")
	if want := "/music/party_keep.csv"; got != want {
		t.Errorf("deriveTournamentOutput = %q, want %q", got, want)
	}
}
