//go:build cgo

package memory_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/embedder"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

func testStore(t *testing.T) *memory.Store {
	t.Helper()
	// Skip if zvec libs not available
	if os.Getenv("CGO_ENABLED") == "0" {
		t.Skip("skipping: cgo disabled")
	}
	dir := t.TempDir()
	store, err := memory.Open(memory.OpenConfig{
		ZvecPath:     filepath.Join(dir, "zvec"),
		EmbeddingDim: 128,
		LockTimeout:  2 * time.Second,
		FTSWeight:    0.5,
		VectorWeight: 0.5,
		Embedder:     embedder.Disabled{},
	})
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestStoreAddAndGet(t *testing.T) {
	t.Parallel()
	store := testStore(t)
	ctx := context.Background()

	record, err := store.Add(ctx, memory.AddRequest{
		Kind:    "fact",
		Summary: "test summary",
		Body:    "test body",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if record.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	got, err := store.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Body != "test body" {
		t.Fatalf("expected 'test body', got %q", got.Body)
	}
}

func TestStoreForget(t *testing.T) {
	t.Parallel()
	store := testStore(t)
	ctx := context.Background()

	record, _ := store.Add(ctx, memory.AddRequest{Body: "to delete"})
	if err := store.Forget(ctx, record.ID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	_, err := store.Get(ctx, record.ID)
	if err != memory.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStoreStatus(t *testing.T) {
	t.Parallel()
	store := testStore(t)
	ctx := context.Background()

	store.Add(ctx, memory.AddRequest{Body: "one"})
	store.Add(ctx, memory.AddRequest{Body: "two"})

	status, err := store.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MemoryCount != 2 {
		t.Fatalf("expected 2 memories, got %d", status.MemoryCount)
	}
	if status.Backend != "zvec" {
		t.Fatalf("expected 'zvec', got %q", status.Backend)
	}
}

// writeJSONLRecords writes a minimal memories.jsonl with the given records into dir.
func writeJSONLRecords(t *testing.T, dir string, records []memory.Record) {
	t.Helper()
	path := filepath.Join(dir, "memories.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jsonl: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			t.Fatalf("encode record: %v", err)
		}
	}
}

// openTestStoreInDir opens a store whose zvec collection lives in dir/zvec.
func openTestStoreInDir(t *testing.T, dir string) *memory.Store {
	t.Helper()
	if os.Getenv("CGO_ENABLED") == "0" {
		t.Skip("skipping: cgo disabled")
	}
	store, err := memory.Open(memory.OpenConfig{
		ZvecPath:     filepath.Join(dir, "zvec"),
		EmbeddingDim: 128,
		LockTimeout:  2 * time.Second,
		FTSWeight:    0.5,
		VectorWeight: 0.5,
		Embedder:     embedder.Disabled{},
	})
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	return store
}

func TestMigrationImportsJSONL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)

	// Write two records whose IDs contain colons (and one with # too).
	records := []memory.Record{
		{ID: "note:alpha", Kind: "fact", Summary: "alpha note", Body: "body alpha", CreatedAt: now, UpdatedAt: now},
		{ID: "session:beta-2026#003", Kind: "fact", Summary: "beta session", Body: "body beta", CreatedAt: now, UpdatedAt: now},
	}
	writeJSONLRecords(t, dir, records)

	store := openTestStoreInDir(t, dir)
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()

	// Collection should have 2 documents after migration.
	status, err := store.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MemoryCount != 2 {
		t.Fatalf("expected 2 memories after migration, got %d", status.MemoryCount)
	}

	// Original jsonl must be gone; .migrated must exist.
	jsonlPath := filepath.Join(dir, "memories.jsonl")
	if _, err := os.Stat(jsonlPath); !os.IsNotExist(err) {
		t.Fatal("expected memories.jsonl to be absent after migration")
	}
	if _, err := os.Stat(jsonlPath + ".migrated"); err != nil {
		t.Fatalf("expected memories.jsonl.migrated to exist: %v", err)
	}

	// Get by original colon IDs must work (sanitizePK applied symmetrically).
	got, err := store.Get(ctx, "note:alpha")
	if err != nil {
		t.Fatalf("get note:alpha: %v", err)
	}
	if got.Body != "body alpha" {
		t.Fatalf("got.Body = %q, want %q", got.Body, "body alpha")
	}

	got2, err := store.Get(ctx, "session:beta-2026#003")
	if err != nil {
		t.Fatalf("get session:beta-2026#003: %v", err)
	}
	if got2.Body != "body beta" {
		t.Fatalf("got2.Body = %q, want %q", got2.Body, "body beta")
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)

	records := []memory.Record{
		{ID: "note:idempotent", Kind: "fact", Summary: "idempotent", Body: "body", CreatedAt: now, UpdatedAt: now},
	}
	writeJSONLRecords(t, dir, records)

	// First open: migration runs, jsonl renamed to .migrated.
	store1 := openTestStoreInDir(t, dir)
	ctx := context.Background()
	status1, err := store1.Status(ctx)
	if err != nil {
		t.Fatalf("status first open: %v", err)
	}
	if status1.MemoryCount != 1 {
		t.Fatalf("expected 1 memory after first open, got %d", status1.MemoryCount)
	}
	store1.Close()

	// Second open: .migrated exists but original jsonl does not -> migration skipped,
	// collection already has docs -> double-idempotent guard.
	store2 := openTestStoreInDir(t, dir)
	t.Cleanup(func() { store2.Close() })
	status2, err := store2.Status(ctx)
	if err != nil {
		t.Fatalf("status second open: %v", err)
	}
	if status2.MemoryCount != status1.MemoryCount {
		t.Fatalf("second open: expected %d memories, got %d (duplication occurred)", status1.MemoryCount, status2.MemoryCount)
	}
}
