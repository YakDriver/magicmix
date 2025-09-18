package track_test

import (
	"testing"

	"github.com/YakDriver/magicmix/internal/track"
)

func TestParseKey(t *testing.T) {
	tests := []struct {
		input string
		want  track.Key
		ok    bool
	}{
		{"1A", track.Key{Number: 1, Mode: track.ModeA}, true},
		{"12b", track.Key{Number: 12, Mode: track.ModeB}, true},
		{" 5B ", track.Key{Number: 5, Mode: track.ModeB}, true},
		{"0A", track.Key{}, false},
		{"13B", track.Key{}, false},
		{"AA", track.Key{}, false},
		{"2", track.Key{}, false},
	}

	for _, tc := range tests {
		got, err := track.ParseKey(tc.input)
		if tc.ok && err != nil {
			t.Fatalf("ParseKey(%q) unexpected error: %v", tc.input, err)
		}
		if !tc.ok {
			if err == nil {
				t.Fatalf("ParseKey(%q) expected error", tc.input)
			}
			continue
		}
		if got != tc.want {
			t.Fatalf("ParseKey(%q) = %+v, want %+v", tc.input, got, tc.want)
		}
	}
}

func TestKeyString(t *testing.T) {
	key := track.Key{Number: 7, Mode: track.ModeA}
	if got := key.String(); got != "7A" {
		t.Fatalf("Key.String() = %s, want 7A", got)
	}
}
