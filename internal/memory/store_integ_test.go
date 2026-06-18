//go:build cgo

package memory_test

import (
	"context"
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
