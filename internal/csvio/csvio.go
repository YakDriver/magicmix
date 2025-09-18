package csvio

import (
	"context"
	"encoding/csv"
	"fmt"
	gosio "io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YakDriver/magicmix/internal/track"
)

// Load reads tracks from a CSV file on disk.
func Load(ctx context.Context, path string) ([]track.Track, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open input: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true

	var (
		tracks []track.Track
		line   int
		header bool
	)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		record, err := reader.Read()
		if err != nil {
			if err == gosio.EOF {
				break
			}
			return nil, fmt.Errorf("read line %d: %w", line+1, err)
		}
		line++
		if len(record) == 0 {
			continue
		}
		if len(record) < 5 {
			return nil, fmt.Errorf("line %d: expected 5 columns but got %d", line, len(record))
		}

		if line == 1 {
			if !looksLikeData(record) {
				header = true
				continue
			}
		}

		if header && line == 1 {
			continue
		}

		track, err := parseRecord(record)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		tracks = append(tracks, track)
	}

	return tracks, nil
}

// Save writes ordered tracks to disk, creating directories as needed.
func Save(_ context.Context, path string, tracks []track.Track) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	header := []string{"Title", "Artist", "BPM", "Energy", "Key"}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	for _, t := range tracks {
		row := []string{
			t.Title,
			t.Artist,
			strconv.FormatFloat(t.BPM, 'f', -1, 64),
			strconv.Itoa(t.Energy),
			t.Key.String(),
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}

	writer.Flush()
	return writer.Error()
}

func looksLikeData(record []string) bool {
	if len(record) < 5 {
		return false
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(record[2]), 64); err != nil {
		return false
	}
	if _, err := strconv.Atoi(strings.TrimSpace(record[3])); err != nil {
		return false
	}
	if _, err := track.ParseKey(record[4]); err != nil {
		return false
	}
	return true
}

func parseRecord(record []string) (track.Track, error) {
	title := strings.TrimSpace(record[0])
	artist := strings.TrimSpace(record[1])

	bpm, err := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
	if err != nil {
		return track.Track{}, fmt.Errorf("invalid bpm: %w", err)
	}

	energy, err := strconv.Atoi(strings.TrimSpace(record[3]))
	if err != nil {
		return track.Track{}, fmt.Errorf("invalid energy: %w", err)
	}
	if energy < 0 || energy > 100 {
		return track.Track{}, fmt.Errorf("energy out of range: %d", energy)
	}

	key, err := track.ParseKey(record[4])
	if err != nil {
		return track.Track{}, err
	}

	return track.Track{
		Title:  title,
		Artist: artist,
		BPM:    bpm,
		Energy: energy,
		Key:    key,
	}, nil
}
