package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/embedder"
)

func TestStoreAddsSearchesGetsAndForgetsMemory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(OpenConfig{
		ZvecPath:     filepath.Join(t.TempDir(), "zvec"),
		EmbeddingDim: 128,
		LockTimeout:  2 * time.Second,
		FTSWeight:    0.5,
		VectorWeight: 0.5,
		Embedder:     embedder.Disabled{},
	})
	if err != nil {
		t.Skipf("skipping: zvec unavailable: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

	record, err := store.Add(ctx, AddRequest{
		ID:      "style",
		Kind:    "feedback",
		Project: "agent-memoryd",
		Body:    "Prefer concise final answers with concrete file links.",
		Now:     now,
	})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	if record.ID != "style" {
		t.Fatalf("record.ID = %q, want style", record.ID)
	}

	got, err := store.Get(ctx, "style")
	if err != nil {
		t.Fatalf("get memory: %v", err)
	}
	if got.Body != record.Body {
		t.Fatalf("got.Body = %q, want %q", got.Body, record.Body)
	}

	if err := store.Forget(ctx, "style"); err != nil {
		t.Fatalf("forget memory: %v", err)
	}
	_, err = store.Get(ctx, "style")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("get forgotten memory error = %v, want ErrNotFound", err)
	}
}
