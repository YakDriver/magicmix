package strategy

import (
	"testing"

	"github.com/YakDriver/magicmix/internal/track"
)

func TestScoreKeyTransitions(t *testing.T) {
	tests := []struct {
		name     string
		keys     []string
		expected KeyScore
	}{
		{
			name: "perfect transitions - adjacent keys, same mode",
			keys: []string{"1A", "2A", "3A", "4A", "5A"},
			expected: KeyScore{
				Total:            0,
				ModeAndNumberPts: 0,
				NumberChangePts:  0,
				RunPenaltyPts:    0,
			},
		},
		{
			name: "mode and number change - harsh penalty",
			keys: []string{"1A", "3B"}, // both change - very difficult transition
			expected: KeyScore{
				Total:            6, // 5 for both changing + 1 for 2-step number jump
				ModeAndNumberPts: 5,
				NumberChangePts:  1,
				RunPenaltyPts:    0,
			},
		},
		{
			name: "two-step number transitions",
			keys: []string{"10A", "8A", "6A"}, // down 2, then down 2
			expected: KeyScore{
				Total:            2, // 1 + 1 for two-step jumps
				ModeAndNumberPts: 0,
				NumberChangePts:  2,
				RunPenaltyPts:    0,
			},
		},
		{
			name: "large number jumps",
			keys: []string{"1A", "4A", "8A"}, // up 3, then up 4
			expected: KeyScore{
				Total:            9, // 3 + 6 for difficult transitions
				ModeAndNumberPts: 0,
				NumberChangePts:  9,
				RunPenaltyPts:    0,
			},
		},
		{
			name: "run penalties",
			keys: []string{"7A", "7A", "7A", "7A", "7A"}, // run of 5
			expected: KeyScore{
				Total:            12, // (5-2)*(5-1) = 3*4 = 12 points
				ModeAndNumberPts: 0,
				NumberChangePts:  0,
				RunPenaltyPts:    12,
			},
		},
		{
			name: "wrap around transitions",
			keys: []string{"12A", "1A", "11A"}, // 12->1 (up 1), 1->11 (down 2)
			expected: KeyScore{
				Total:            1, // 0 + 1 (adjacent good, 2-step penalty)
				ModeAndNumberPts: 0,
				NumberChangePts:  1,
				RunPenaltyPts:    0,
			},
		},
		{
			name: "realistic distribution example",
			keys: []string{
				"1A", "2A", "3A", "4A", "5A", "6A", "7A", "8A", "9A", "10A", "10A", "10A", "11A", "12A",
			},
			expected: KeyScore{
				Total:         2, // run of 3 10A's = (3-2)*(3-1) = 1*2 = 2 points
				RunPenaltyPts: 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracks := make([]track.Track, len(tt.keys))
			for i, keyStr := range tt.keys {
				key, err := track.ParseKey(keyStr)
				if err != nil {
					t.Fatalf("Failed to parse key %s: %v", keyStr, err)
				}
				tracks[i] = track.Track{
					Title:  "Test Track",
					Artist: "Test Artist",
					Key:    key,
				}
			}

			result := ScoreKeyTransitions(tracks)

			if result.Total != tt.expected.Total {
				t.Errorf("Total score = %d, expected %d", result.Total, tt.expected.Total)
			}
			if result.ModeAndNumberPts != tt.expected.ModeAndNumberPts {
				t.Errorf("ModeAndNumberPts = %d, expected %d", result.ModeAndNumberPts, tt.expected.ModeAndNumberPts)
			}
			if result.NumberChangePts != tt.expected.NumberChangePts {
				t.Errorf("NumberChangePts = %d, expected %d", result.NumberChangePts, tt.expected.NumberChangePts)
			}
			if result.RunPenaltyPts != tt.expected.RunPenaltyPts {
				t.Errorf("RunPenaltyPts = %d, expected %d", result.RunPenaltyPts, tt.expected.RunPenaltyPts)
			}
		})
	}
}

func TestScoreKeyRun(t *testing.T) {
	tests := []struct {
		runLength int
		expected  int
	}{
		{1, 0}, {2, 0}, // acceptable runs
		{3, 2},  // (3-2)*(3-1) = 1*2 = 2
		{4, 6},  // (4-2)*(4-1) = 2*3 = 6
		{5, 12}, // (5-2)*(5-1) = 3*4 = 12
		{6, 20}, // (6-2)*(6-1) = 4*5 = 20
	}

	for _, tt := range tests {
		result := scoreKeyRun(tt.runLength)
		if result != tt.expected {
			t.Errorf("scoreKeyRun(%d) = %d, expected %d", tt.runLength, result, tt.expected)
		}
	}
}

func TestScoreKeyNumberChange(t *testing.T) {
	tests := []struct {
		fromKey, toKey string
		expected       int
	}{
		// Perfect matches and adjacent keys (harmonic)
		{"1A", "1A", 0},  // same key
		{"1A", "2A", 0},  // adjacent up
		{"2A", "1A", 0},  // adjacent down
		{"12A", "1A", 0}, // wrap around adjacent
		{"1A", "12A", 0}, // wrap around adjacent

		// Two steps - acceptable but requires skill
		{"1A", "3A", 1},  // up 2
		{"3A", "1A", 1},  // down 2
		{"11A", "1A", 1}, // wrap around 2

		// Three steps - difficult transition
		{"1A", "4A", 3}, // up 3
		{"4A", "1A", 3}, // down 3

		// Four steps - very harsh
		{"1A", "5A", 6}, // up 4
		{"5A", "1A", 6}, // down 4

		// Five steps - extremely difficult
		{"1A", "6A", 8}, // up 5

		// Tritone - worst possible
		{"1A", "7A", 10}, // up 6 (tritone)
		{"7A", "1A", 10}, // down 6 (tritone)
	}

	for _, tt := range tests {
		fromKey, err := track.ParseKey(tt.fromKey)
		if err != nil {
			t.Fatalf("Failed to parse from key %s: %v", tt.fromKey, err)
		}
		toKey, err := track.ParseKey(tt.toKey)
		if err != nil {
			t.Fatalf("Failed to parse to key %s: %v", tt.toKey, err)
		}

		result := scoreKeyNumberChange(fromKey, toKey)
		if result != tt.expected {
			t.Errorf("scoreKeyNumberChange(%s, %s) = %d, expected %d", tt.fromKey, tt.toKey, result, tt.expected)
		}
	}
}
