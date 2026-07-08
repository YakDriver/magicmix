package csvio

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YakDriver/magicmix/internal/track"
)

// Playlist is a track list plus the shape of the file it came from, so output can be
// written back in the same format (same columns, same order, extra columns preserved).
type Playlist struct {
	Header []string // the input header row; nil when the file had no recognizable header
	CRLF   bool     // the input used \r\n line endings
	Tracks []track.Track
}

// Load reads tracks from a CSV file on disk. It is a convenience wrapper around
// LoadPlaylist for callers that only need the tracks.
//
// The reader is header-aware: it maps columns by name (case-insensitive, tolerant
// of punctuation such as "POP.") so column order and extra/unknown columns do not
// matter. Recognized optional signals (danceability, valence, popularity,
// acousticness, length, release year) are captured when present. Files without a
// recognizable header fall back to the legacy positional layout: title, artist, bpm,
// energy, key.
func Load(ctx context.Context, path string) ([]track.Track, error) {
	pl, err := LoadPlaylist(ctx, path)
	if err != nil {
		return nil, err
	}
	return pl.Tracks, nil
}

// LoadPlaylist reads a CSV and also remembers the header row and line-ending style,
// and stashes each row's raw cells on its Track, so SaveInFormat can echo the input's
// exact columns and order with only the rows reordered.
func LoadPlaylist(ctx context.Context, path string) (Playlist, error) {
	if err := ctx.Err(); err != nil {
		return Playlist{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Playlist{}, fmt.Errorf("open input: %w", err)
	}
	pl := Playlist{CRLF: bytes.Contains(data, []byte("\r\n"))}

	reader := csv.NewReader(bytes.NewReader(data))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1 // tolerate rows with differing column counts

	var records [][]string
	for {
		if err := ctx.Err(); err != nil {
			return Playlist{}, err
		}
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Playlist{}, fmt.Errorf("read csv: %w", err)
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return pl, nil
	}

	if columns, ok := detectHeader(records[0]); ok {
		pl.Header = records[0]
		tracks, err := parseMapped(records[1:], columns)
		if err != nil {
			return Playlist{}, err
		}
		pl.Tracks = tracks
		return pl, nil
	}

	tracks, err := parsePositional(records)
	if err != nil {
		return Playlist{}, err
	}
	pl.Tracks = tracks
	return pl, nil
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
	colYear
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
	"release": colYear, "released": colYear, "year": colYear,
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
		tr.Raw = record
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
	tr.Year = optionalYear(field(colYear))
	return tr, nil
}

// optionalYear extracts a 4-digit release year from values like "2024-05-01" or
// "2024", returning nil when absent or unparseable.
func optionalYear(s string, present bool) *int {
	if !present {
		return nil
	}
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return nil
	}
	year, err := strconv.Atoi(s[:4])
	if err != nil || year < 1900 || year > 2200 {
		return nil
	}
	return &year
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
		tr.Raw = record
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

// Save writes ordered tracks to disk in magicmix's canonical schema, creating
// directories as needed.
func Save(_ context.Context, path string, tracks []track.Track) error {
	return writeCSV(path, false, func(w *csv.Writer) error {
		return writeCanonical(w, tracks)
	})
}

// SaveInFormat writes pl.Tracks to disk. When the tracks carry their original rows
// (loaded via LoadPlaylist), it echoes the input's exact columns and order — header,
// extra columns, and line endings — with only the rows reordered. Otherwise it falls
// back to the canonical schema.
func SaveInFormat(_ context.Context, path string, pl Playlist) error {
	passthrough := len(pl.Tracks) > 0 && allHaveRaw(pl.Tracks)
	return writeCSV(path, pl.CRLF && passthrough, func(w *csv.Writer) error {
		if !passthrough {
			return writeCanonical(w, pl.Tracks)
		}
		if pl.Header != nil {
			if err := w.Write(pl.Header); err != nil {
				return fmt.Errorf("write header: %w", err)
			}
		}
		for _, t := range pl.Tracks {
			if err := w.Write(t.Raw); err != nil {
				return fmt.Errorf("write row: %w", err)
			}
		}
		return nil
	})
}

func allHaveRaw(tracks []track.Track) bool {
	for _, t := range tracks {
		if t.Raw == nil {
			return false
		}
	}
	return true
}

// writeCSV opens path (creating parent dirs), hands a writer to write, then flushes.
func writeCSV(path string, useCRLF bool, write func(*csv.Writer) error) (err error) {
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
	writer.UseCRLF = useCRLF
	if werr := write(writer); werr != nil {
		return werr
	}
	writer.Flush()
	return writer.Error()
}

// writeCanonical writes tracks in magicmix's own schema: the core five columns plus
// whichever optional signals any track carries.
func writeCanonical(writer *csv.Writer, tracks []track.Track) error {
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
	var hasYear bool
	for _, t := range tracks {
		if t.Year != nil {
			hasYear = true
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
	if hasYear {
		header = append(header, "Release")
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
		if hasYear {
			row = append(row, optIntString(t.Year))
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}
	return nil
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
