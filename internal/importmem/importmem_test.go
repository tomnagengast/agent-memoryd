package importmem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

func TestImportJSONLPreservesRecordFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "memories.jsonl")
	if err := os.WriteFile(source, []byte(`{"id":"custom-id","kind":"preference","project":"agent-memoryd","source":"old","summary":"Keep init scriptable","body":"Init should stay scriptable while offering interactive setup."}`+"\n"), 0o644); err != nil {
		t.Fatalf("write import source: %v", err)
	}
	store := memory.NewStore(filepath.Join(dir, "store.jsonl"))

	result, err := Import(ctx, store, Options{Path: source})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Imported != 1 || result.Skipped != 0 || result.Format != "jsonl" {
		t.Fatalf("result = %#v, want one jsonl import", result)
	}
	record, err := store.Get(ctx, "custom-id")
	if err != nil {
		t.Fatalf("get imported record: %v", err)
	}
	if record.Kind != "preference" || record.Project != "agent-memoryd" || record.Source != "old" {
		t.Fatalf("record = %#v, want preserved fields", record)
	}
}

func TestImportMarkdownDirectoryUsesStableIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	note := filepath.Join(dir, "note.md")
	if err := os.WriteFile(note, []byte("# Durable setup\n\nImport existing notes during init."), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	store := memory.NewStore(filepath.Join(t.TempDir(), "store.jsonl"))

	for i := 0; i < 2; i++ {
		result, err := Import(ctx, store, Options{Path: dir, Project: "example"})
		if err != nil {
			t.Fatalf("import %d: %v", i, err)
		}
		if result.Imported != 1 {
			t.Fatalf("import %d result = %#v, want one import", i, result)
		}
	}
	records, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want stable reimport to update one record", len(records))
	}
	if records[0].Kind != "note" || records[0].Project != "example" || records[0].Summary != "Durable setup" {
		t.Fatalf("record = %#v, want imported markdown note", records[0])
	}
}
