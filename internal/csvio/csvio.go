package csvio

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YakDriver/magicmix/internal/track"
)

// Load reads tracks from a CSV file on disk.
//
// The reader is header-aware: it maps columns by name (case-insensitive, tolerant
// of punctuation such as "POP.") so column order and extra/unknown columns do not
// matter. Recognized optional signals (danceability, valence, popularity,
// acousticness) are captured when present. Files without a recognizable header fall
// back to the legacy positional layout: title, artist, bpm, energy, key.
func Load(ctx context.Context, path string) ([]track.Track, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open input: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1 // tolerate rows with differing column counts

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}

	if columns, ok := detectHeader(records[0]); ok {
		return parseMapped(records[1:], columns)
	}
	return parsePositional(records)
}

// column identifies a canonical field the reader knows how to use.
type column int

const (
	colTitle column = iota
	colArtist
	colBPM
	colEnergy
	colKey
	colDanceability
	colValence
	colPopularity
	colAcousticness
	colLength
)

// columnSynonyms maps normalized header names to canonical columns.
var columnSynonyms = map[string]column{
	"title": colTitle, "song": colTitle, "track": colTitle, "name": colTitle,
	"artist": colArtist, "artists": colArtist,
	"bpm": colBPM, "tempo": colBPM,
	"energy": colEnergy,
	"key":    colKey, "camelot": colKey,
	"dance": colDanceability, "danceability": colDanceability,
	"valence": colValence, "mood": colValence,
	"pop": colPopularity, "popularity": colPopularity,
	"acoustic": colAcousticness, "acousticness": colAcousticness,
	"length": colLength, "duration": colLength, "len": colLength,
}

// normalizeHeader lowercases and strips surrounding spaces and trailing dots so
// headers like "POP." or " Key " map cleanly.
func normalizeHeader(s string) string {
	return strings.TrimRight(strings.TrimSpace(strings.ToLower(s)), ".")
}

// detectHeader builds a column index from a row of header names. It only reports a
// header when the core ordering signals (title, bpm, energy, key) are all present.
func detectHeader(row []string) (map[column]int, bool) {
	columns := make(map[column]int)
	for i, cell := range row {
		if c, ok := columnSynonyms[normalizeHeader(cell)]; ok {
			if _, exists := columns[c]; !exists {
				columns[c] = i
			}
		}
	}
	for _, required := range []column{colTitle, colBPM, colEnergy, colKey} {
		if _, ok := columns[required]; !ok {
			return nil, false
		}
	}
	return columns, true
}

func parseMapped(rows [][]string, columns map[column]int) ([]track.Track, error) {
	tracks := make([]track.Track, 0, len(rows))
	for i, record := range rows {
		if isBlank(record) {
			continue
		}
		tr, err := recordToTrack(record, columns)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+2, err) // +2: header is line 1
		}
		tracks = append(tracks, tr)
	}
	return tracks, nil
}

func recordToTrack(record []string, columns map[column]int) (track.Track, error) {
	field := func(c column) (string, bool) {
		j, ok := columns[c]
		if !ok || j >= len(record) {
			return "", false
		}
		return strings.TrimSpace(record[j]), true
	}

	title, _ := field(colTitle)
	artist, _ := field(colArtist)

	bpmStr, _ := field(colBPM)
	bpm, err := strconv.ParseFloat(bpmStr, 64)
	if err != nil {
		return track.Track{}, fmt.Errorf("invalid bpm %q: %w", bpmStr, err)
	}

	energyStr, _ := field(colEnergy)
	energy, err := parseScale(energyStr)
	if err != nil {
		return track.Track{}, fmt.Errorf("invalid energy: %w", err)
	}

	keyStr, _ := field(colKey)
	key, err := track.ParseKey(keyStr)
	if err != nil {
		return track.Track{}, err
	}

	tr := track.Track{Title: title, Artist: artist, BPM: bpm, Energy: energy, Key: key}
	tr.Danceability = optionalScale(field(colDanceability))
	tr.Valence = optionalScale(field(colValence))
	tr.Popularity = optionalScale(field(colPopularity))
	tr.Acousticness = optionalScale(field(colAcousticness))
	tr.Duration = optionalDuration(field(colLength))
	return tr, nil
}

// parseDuration parses a track length such as "3:17" (m:ss) or "1:02:03" (h:mm:ss),
// or a plain seconds count, into seconds.
func parseDuration(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	parts := strings.Split(s, ":")
	total := 0
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 0 {
			return 0, false
		}
		total = total*60 + n
	}
	return total, true
}

// optionalDuration parses an optional track length, returning nil when absent or
// unparseable.
func optionalDuration(s string, present bool) *int {
	if !present {
		return nil
	}
	if sec, ok := parseDuration(s); ok {
		return &sec
	}
	return nil
}

// parseScale parses a required 0-100 integer signal.
func parseScale(s string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	if v < 0 || v > 100 {
		return 0, fmt.Errorf("value out of range 0-100: %d", v)
	}
	return v, nil
}

// optionalScale parses an optional 0-100 signal, returning nil when the value is
// absent or unparseable so scoring simply skips it.
func optionalScale(s string, present bool) *int {
	if !present || s == "" {
		return nil
	}
	v, err := parseScale(s)
	if err != nil {
		return nil
	}
	return &v
}

func parsePositional(records [][]string) ([]track.Track, error) {
	tracks := make([]track.Track, 0, len(records))
	for i, record := range records {
		if isBlank(record) {
			continue
		}
		if len(record) < 5 {
			return nil, fmt.Errorf("line %d: expected 5 columns but got %d", i+1, len(record))
		}
		if i == 0 && !looksLikeData(record) {
			continue // legacy header row
		}
		tr, err := parseRecord(record)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		tracks = append(tracks, tr)
	}
	return tracks, nil
}

func isBlank(record []string) bool {
	for _, field := range record {
		if strings.TrimSpace(field) != "" {
			return false
		}
	}
	return true
}

// Save writes ordered tracks to disk, creating directories as needed.
func Save(_ context.Context, path string, tracks []track.Track) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close output: %w", cerr)
		}
	}()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Preserve optional signals only when at least one track carries them, so
	// legacy 5-column files round-trip unchanged while rich files keep their data.
	var hasDance, hasValence, hasPop, hasAcoustic bool
	for _, t := range tracks {
		hasDance = hasDance || t.Danceability != nil
		hasValence = hasValence || t.Valence != nil
		hasPop = hasPop || t.Popularity != nil
		hasAcoustic = hasAcoustic || t.Acousticness != nil
	}
	var hasLength bool
	for _, t := range tracks {
		if t.Duration != nil {
			hasLength = true
			break
		}
	}

	header := []string{"Title", "Artist", "BPM", "Energy", "Key"}
	if hasDance {
		header = append(header, "Danceability")
	}
	if hasValence {
		header = append(header, "Valence")
	}
	if hasPop {
		header = append(header, "Popularity")
	}
	if hasAcoustic {
		header = append(header, "Acousticness")
	}
	if hasLength {
		header = append(header, "Length")
	}
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
		if hasDance {
			row = append(row, optIntString(t.Danceability))
		}
		if hasValence {
			row = append(row, optIntString(t.Valence))
		}
		if hasPop {
			row = append(row, optIntString(t.Popularity))
		}
		if hasAcoustic {
			row = append(row, optIntString(t.Acousticness))
		}
		if hasLength {
			row = append(row, formatDuration(t.Duration))
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}

	writer.Flush()
	return writer.Error()
}

// optIntString renders an optional signal, using an empty cell when absent.
func optIntString(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

// formatDuration renders seconds as m:ss (or h:mm:ss), empty when absent.
func formatDuration(p *int) string {
	if p == nil {
		return ""
	}
	sec := *p
	h, m, s := sec/3600, (sec%3600)/60, sec%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
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
