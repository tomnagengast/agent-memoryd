package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseJSONLMissingFile(t *testing.T) {
	t.Parallel()
	records, err := parseJSONL("/nonexistent/path.jsonl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Fatal("expected empty records")
	}
}

func TestParseJSONLValidRecords(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	content := `{"id":"1","kind":"fact","summary":"test","body":"hello","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}
{"id":"2","kind":"fact","summary":"unicode","body":"日本語","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	records, err := parseJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[1].Body != "日本語" {
		t.Fatalf("unicode not preserved: %s", records[1].Body)
	}
}

func TestParseJSONLSkipsBlankLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	content := `{"id":"1","kind":"fact","summary":"s","body":"b","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}

{"id":"2","kind":"fact","summary":"s","body":"b","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	records, err := parseJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
}
