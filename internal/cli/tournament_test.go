package cli

import (
	"testing"

	"github.com/YakDriver/magicmix/internal/tournament"
	"github.com/YakDriver/magicmix/internal/track"
)

func TestDecodeKey(t *testing.T) {
	cases := []struct {
		key  byte
		want tournament.Outcome
		ok   bool
	}{
		{'1', tournament.PickLeft, true},
		{'2', tournament.PickRight, true},
		{'3', tournament.PickBoth, true},
		{'b', tournament.PickBoth, true},
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

func TestSongMeta(t *testing.T) {
	dur, yr, dnc, val, aco, pop := 204, 2019, 63, 45, 12, 88
	full := track.Track{
		BPM: 128, Energy: 78, Key: track.Key{Number: 8, Mode: track.ModeA},
		Duration: &dur, Year: &yr, Danceability: &dnc, Valence: &val, Acousticness: &aco, Popularity: &pop,
	}
	if got, want := songMeta(full), "8A · 128bpm · 3:24 · 2019 · nrg78 dnc63 val45 aco12 pop88"; got != want {
		t.Errorf("songMeta full:\n got %q\nwant %q", got, want)
	}

	bare := track.Track{BPM: 120, Energy: 50, Key: track.Key{Number: 1, Mode: track.ModeB}}
	if got, want := songMeta(bare), "1B · 120bpm · nrg50"; got != want {
		t.Errorf("songMeta bare = %q, want %q", got, want)
	}
}
