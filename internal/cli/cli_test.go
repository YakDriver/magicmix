package cli

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
)

func TestRunWithLimit(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "tracks.csv")
	output := filepath.Join(dir, "out.csv")

	writeCSV(t, input, [][]string{
		{"Title", "Artist", "BPM", "Energy", "Key"},
		{"Track1", "Artist1", "120", "50", "1A"},
		{"Track2", "Artist2", "121", "60", "2A"},
		{"Track3", "Artist3", "122", "70", "3A"},
	})

	args := []string{
		"--input", input,
		"--output", output,
		"--limit", "2",
	}

	if err := run(context.Background(), args); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	rows := readCSV(t, output)
	if len(rows) != 3 { // header + 2 data rows
		t.Fatalf("expected 3 rows in output, got %d", len(rows))
	}
}

func TestRunWithNegativeLimit(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "tracks.csv")
	output := filepath.Join(dir, "out.csv")

	writeCSV(t, input, [][]string{
		{"Title", "Artist", "BPM", "Energy", "Key"},
		{"Track1", "Artist1", "120", "50", "1A"},
	})

	args := []string{
		"--input", input,
		"--output", output,
		"--limit", "-1",
	}

	if err := run(context.Background(), args); err == nil {
		t.Fatalf("expected error for negative limit")
	}
}

func writeCSV(t *testing.T, path string, rows [][]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	if err := writer.WriteAll(rows); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	data, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return data
}
