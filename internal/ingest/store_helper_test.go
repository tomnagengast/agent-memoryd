package ingest

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/embedder"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

func newTestStore(t *testing.T) *memory.Store {
	t.Helper()
	store, err := memory.Open(memory.OpenConfig{
		ZvecPath:     filepath.Join(t.TempDir(), "zvec"),
		EmbeddingDim: 128,
		LockTimeout:  2 * time.Second,
		Embedder:     embedder.Disabled{},
	})
	if err != nil {
		t.Skipf("skipping: zvec unavailable: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
