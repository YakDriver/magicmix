package track

import (
	"fmt"
	"strconv"
	"strings"
)

// Mode identifies whether a key is in the "A" (minor) or "B" (major) side of the Camelot wheel.
type Mode string

const (
	ModeA Mode = "A"
	ModeB Mode = "B"
)

// Key represents a Camelot key such as 1A or 5B.
type Key struct {
	Number int
	Mode   Mode
}

// ParseKey converts a string such as 1A into a Key instance.
func ParseKey(input string) (Key, error) {
	cleaned := strings.TrimSpace(strings.ToUpper(input))
	if len(cleaned) < 2 || len(cleaned) > 3 {
		return Key{}, fmt.Errorf("invalid key format: %q", input)
	}

	mode := Mode(cleaned[len(cleaned)-1:])
	if mode != ModeA && mode != ModeB {
		return Key{}, fmt.Errorf("invalid key mode: %q", input)
	}

	numberPart := cleaned[:len(cleaned)-1]
	number, err := strconv.Atoi(numberPart)
	if err != nil {
		return Key{}, fmt.Errorf("invalid key number: %w", err)
	}
	if number < 1 || number > 12 {
		return Key{}, fmt.Errorf("key number out of range: %d", number)
	}

	return Key{Number: number, Mode: mode}, nil
}

func (k Key) String() string {
	if k.Number == 0 {
		return ""
	}
	return fmt.Sprintf("%d%s", k.Number, string(k.Mode))
}

// Track describes a single audio track row read from input.
//
// Title, Artist, BPM, Energy, and Key are the core signals every strategy relies
// on. The remaining fields are optional extended signals: a nil pointer means the
// source data did not provide that signal, and scoring should skip it rather than
// assume a value.
type Track struct {
	Title  string
	Artist string
	BPM    float64
	Energy int
	Key    Key

	Danceability *int // 0-100, higher = more danceable
	Valence      *int // 0-100, higher = more positive/happy in mood
	Popularity   *int // 0-100, higher = more popular
	Acousticness *int // 0-100, higher = more acoustic
	Duration     *int // track length in seconds
}

// Clone returns a copy useful for preserving the original slice whilst sorting.
// Optional signal pointers are deep-copied so callers never alias the originals.
func (t Track) Clone() Track {
	clone := Track{
		Title:  t.Title,
		Artist: t.Artist,
		BPM:    t.BPM,
		Energy: t.Energy,
		Key:    t.Key,
	}
	clone.Danceability = copyIntPtr(t.Danceability)
	clone.Valence = copyIntPtr(t.Valence)
	clone.Popularity = copyIntPtr(t.Popularity)
	clone.Acousticness = copyIntPtr(t.Acousticness)
	clone.Duration = copyIntPtr(t.Duration)
	return clone
}

func copyIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
