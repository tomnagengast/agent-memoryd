//go:build cgo

package memory_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/embedder"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

// fakeEmbedder returns a constant dim-length vector for any input.
type fakeEmbedder struct{ dim int }

func (f fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	vec := make([]float32, f.dim)
	for i := range vec {
		vec[i] = 0.1
	}
	return vec, nil
}

// switchableEmbedder is an embedder that starts disabled and can be enabled.
type switchableEmbedder struct {
	dim     int
	enabled bool
}

func (s *switchableEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	if !s.enabled {
		return nil, embedder.ErrDisabled
	}
	vec := make([]float32, s.dim)
	for i := range vec {
		vec[i] = 0.1
	}
	return vec, nil
}

type keywordEmbedder struct{ dim int }

func (k keywordEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vec := make([]float32, k.dim)
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "arborist") || strings.Contains(lower, "canopy") || strings.Contains(lower, "tree doctor"):
		vec[0] = 1
	case strings.Contains(lower, "ferment") || strings.Contains(lower, "yeast") || strings.Contains(lower, "wine tank"):
		vec[1] = 1
	case strings.Contains(lower, "keyboard") || strings.Contains(lower, "typing"):
		vec[2] = 1
	default:
		vec[3] = 1
	}
	return vec, nil
}

type swappableKeywordEmbedder struct {
	dim     int
	swapped bool
}

func (s *swappableKeywordEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vec, err := (keywordEmbedder{dim: s.dim}).Embed(ctx, text)
	if err != nil || !s.swapped {
		return vec, err
	}
	vec[0], vec[1] = vec[1], vec[0]
	return vec, nil
}

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

func TestStoreForgetRemovesRecordFromSearch(t *testing.T) {
	t.Parallel()
	if os.Getenv("CGO_ENABLED") == "0" {
		t.Skip("skipping: cgo disabled")
	}
	dir := t.TempDir()
	store, err := memory.Open(memory.OpenConfig{
		ZvecPath:     filepath.Join(dir, "zvec"),
		EmbeddingDim: 768,
		LockTimeout:  2 * time.Second,
		FTSWeight:    0.5,
		VectorWeight: 0.5,
		Embedder:     keywordEmbedder{dim: 768},
	})
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	deleted, err := store.Add(ctx, memory.AddRequest{
		ID:      "forget:arborist",
		Summary: "Tree doctor stale search target",
		Body:    "An arborist assessed a street tree with drought stress.",
	})
	if err != nil {
		t.Fatalf("add deleted target: %v", err)
	}
	if _, err := store.Add(ctx, memory.AddRequest{
		ID:      "keep:fermentation",
		Summary: "Fermentation vessel sanitation",
		Body:    "Clean the wine tank before pitching yeast.",
	}); err != nil {
		t.Fatalf("add survivor: %v", err)
	}
	if err := store.Forget(ctx, deleted.ID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if _, err := store.Get(ctx, deleted.ID); err != memory.ErrNotFound {
		t.Fatalf("get forgotten memory error = %v, want ErrNotFound", err)
	}

	results, err := store.Search(ctx, memory.SearchRequest{
		Query: "tree doctor",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, result := range results {
		if result.ID == deleted.ID {
			t.Fatalf("forgotten memory returned from search: %+v", results)
		}
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
	if status.PendingEmbedding != 2 {
		t.Fatalf("expected 2 pending embeddings (Disabled embedder), got %d", status.PendingEmbedding)
	}

	// List must return same count as GetStats.
	records, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("list: expected 2 records, got %d", len(records))
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

// TestBackfillEmbedsNullVectors verifies the Backfill / pendingCount / Status embedder-probe flow.
//
// Uses a switchable embedder (started disabled, then enabled) on a single store instance to
// avoid the zvec FTS close-reopen limitation present in v0.5.0: FTS queries return 0 results
// when an existing collection is opened in the same process after records were written.
//
// Phase 1 (embedder disabled): Add a record -> PendingEmbedding==1, Configured==false.
// Phase 2 (embedder enabled): Backfill returns 1 -> PendingEmbedding==0,
// Configured==true, OK==true, Dimension==128.
func TestBackfillEmbedsNullVectors(t *testing.T) {
	t.Parallel()
	if os.Getenv("CGO_ENABLED") == "0" {
		t.Skip("skipping: cgo disabled")
	}
	dir := t.TempDir()
	ctx := context.Background()

	// Open with a switchable embedder that starts disabled.
	sw := &switchableEmbedder{dim: 128, enabled: false}
	store, err := memory.Open(memory.OpenConfig{
		ZvecPath:     filepath.Join(dir, "zvec"),
		EmbeddingDim: 128,
		LockTimeout:  2 * time.Second,
		FTSWeight:    0.5,
		VectorWeight: 0.5,
		Embedder:     sw,
	})
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// --- Phase 1: embedder disabled -> Add stores a record with null embedding ---
	_, addErr := store.Add(ctx, memory.AddRequest{
		Summary: "pending embedding test",
		Body:    "this record has no embedding yet",
	})
	if addErr != nil {
		t.Fatalf("add: %v", addErr)
	}
	status1, err := store.Status(ctx)
	if err != nil {
		t.Fatalf("status phase1: %v", err)
	}
	if status1.PendingEmbedding != 1 {
		t.Fatalf("phase1: expected PendingEmbedding=1, got %d", status1.PendingEmbedding)
	}
	if status1.Embedder.Configured {
		t.Fatalf("phase1: expected Embedder.Configured=false with disabled embedder")
	}

	// --- Phase 2: enable embedder -> Backfill should embed the pending record ---
	sw.enabled = true

	n, err := store.Backfill(ctx)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if n != 1 {
		t.Fatalf("backfill: expected 1 embedded, got %d", n)
	}

	status2, err := store.Status(ctx)
	if err != nil {
		t.Fatalf("status phase2: %v", err)
	}
	if status2.PendingEmbedding != 0 {
		t.Fatalf("phase2: expected PendingEmbedding=0, got %d", status2.PendingEmbedding)
	}
	if !status2.Embedder.Configured {
		t.Fatalf("phase2: expected Embedder.Configured=true")
	}
	if !status2.Embedder.OK {
		t.Fatalf("phase2: expected Embedder.OK=true")
	}
	if status2.Embedder.Dimension != 128 {
		t.Fatalf("phase2: expected Embedder.Dimension=128, got %d", status2.Embedder.Dimension)
	}
}

func TestEmbeddedAddVectorSearchPersistsAcrossProcesses(t *testing.T) {
	if os.Getenv("MEMORYD_VECTOR_PERSIST_HELPER") != "" {
		runVectorPersistHelper(t)
		return
	}
	if os.Getenv("CGO_ENABLED") == "0" {
		t.Skip("skipping: cgo disabled")
	}
	dir := t.TempDir()
	for _, phase := range []string{"write", "read"} {
		cmd := exec.Command(os.Args[0], "-test.run=TestEmbeddedAddVectorSearchPersistsAcrossProcesses", "--", dir, phase)
		cmd.Env = append(os.Environ(), "MEMORYD_VECTOR_PERSIST_HELPER=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s helper failed: %v\n%s", phase, err, out)
		}
	}
}

func TestFilteredVectorSearchFindsProjectResultWithDistractors(t *testing.T) {
	t.Parallel()
	if os.Getenv("CGO_ENABLED") == "0" {
		t.Skip("skipping: cgo disabled")
	}
	dir := t.TempDir()
	store, err := memory.Open(memory.OpenConfig{
		ZvecPath:     filepath.Join(dir, "zvec"),
		EmbeddingDim: 768,
		LockTimeout:  2 * time.Second,
		FTSWeight:    0,
		VectorWeight: 1,
		Embedder:     keywordEmbedder{dim: 768},
	})
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		_, err := store.Add(ctx, memory.AddRequest{
			ID:      "distractor-" + strconv.Itoa(i),
			Project: "other",
			Summary: "Tree doctor distractor",
			Body:    "An arborist can diagnose a sick street tree.",
		})
		if err != nil {
			t.Fatalf("add distractor %d: %v", i, err)
		}
	}
	_, err = store.Add(ctx, memory.AddRequest{
		ID:      "target:arborist",
		Project: "target",
		Summary: "Sycamore canopy inspection",
		Body:    "An arborist assessed a street tree with drought stress.",
	})
	if err != nil {
		t.Fatalf("add target: %v", err)
	}
	results, err := store.Search(ctx, memory.SearchRequest{
		Project: "target",
		Query:   "tree doctor",
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected filtered vector result, got none")
	}
	if results[0].ID != "target_arborist" {
		t.Fatalf("top result = %s, want target_arborist; results=%+v", results[0].ID, results)
	}
}

func TestFilteredVectorSearchRecomputesFilteredEmbeddings(t *testing.T) {
	t.Parallel()
	if os.Getenv("CGO_ENABLED") == "0" {
		t.Skip("skipping: cgo disabled")
	}
	dir := t.TempDir()
	emb := &swappableKeywordEmbedder{dim: 768, swapped: true}
	store, err := memory.Open(memory.OpenConfig{
		ZvecPath:     filepath.Join(dir, "zvec"),
		EmbeddingDim: 768,
		LockTimeout:  2 * time.Second,
		FTSWeight:    0,
		VectorWeight: 1,
		Embedder:     emb,
	})
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	_, err = store.Add(ctx, memory.AddRequest{
		ID:      "target:z-arborist",
		Project: "target",
		Summary: "Sycamore canopy inspection",
		Body:    "An arborist assessed a street tree with drought stress.",
	})
	if err != nil {
		t.Fatalf("add arborist: %v", err)
	}
	_, err = store.Add(ctx, memory.AddRequest{
		ID:      "target:a-fermentation",
		Project: "target",
		Summary: "Fermentation vessel sanitation",
		Body:    "Clean the wine tank before pitching yeast.",
	})
	if err != nil {
		t.Fatalf("add fermentation: %v", err)
	}
	emb.swapped = false
	results, err := store.Search(ctx, memory.SearchRequest{
		Project: "target",
		Query:   "tree doctor",
		Limit:   2,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected filtered vector result, got none")
	}
	if results[0].ID != "target_z-arborist" {
		t.Fatalf("top result = %s, want target_z-arborist; results=%+v", results[0].ID, results)
	}
}

func runVectorPersistHelper(t *testing.T) {
	t.Helper()
	args := os.Args
	if len(args) < 3 {
		t.Fatal("missing helper args")
	}
	dir, phase := args[len(args)-2], args[len(args)-1]
	store, err := memory.Open(memory.OpenConfig{
		ZvecPath:     filepath.Join(dir, "zvec"),
		EmbeddingDim: 768,
		LockTimeout:  2 * time.Second,
		FTSWeight:    0,
		VectorWeight: 1,
		Embedder:     keywordEmbedder{dim: 768},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	switch phase {
	case "write":
		_, err := store.Add(ctx, memory.AddRequest{
			ID:      "persist:arborist",
			Project: "vector-persist",
			Summary: "Sycamore canopy inspection",
			Body:    "An arborist assessed a street tree with drought stress.",
		})
		if err != nil {
			t.Fatalf("add arborist: %v", err)
		}
		_, err = store.Add(ctx, memory.AddRequest{
			ID:      "persist:fermentation",
			Project: "vector-persist",
			Summary: "Fermentation vessel sanitation",
			Body:    "Clean the wine tank before pitching yeast.",
		})
		if err != nil {
			t.Fatalf("add fermentation: %v", err)
		}
		if err := store.Optimize(ctx); err != nil {
			t.Fatalf("optimize: %v", err)
		}
	case "read":
		results, err := store.Search(ctx, memory.SearchRequest{
			Project: "vector-persist",
			Query:   "tree doctor",
			Limit:   2,
		})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected vector results after reopen, got none")
		}
		if results[0].ID != "persist_arborist" {
			t.Fatalf("top result = %s, want persist_arborist; results=%+v", results[0].ID, results)
		}
	default:
		t.Fatalf("unknown helper phase %q", phase)
	}
}

// TestSearchFTSLegWithoutEmbedder verifies that FTS search works even with no embedder.
func TestSearchFTSLegWithoutEmbedder(t *testing.T) {
	t.Parallel()
	store := testStore(t) // uses embedder.Disabled
	ctx := context.Background()

	_, err := store.Add(ctx, memory.AddRequest{
		Summary: "golang channel patterns",
		Body:    "select statement over multiple channels in Go",
	})
	if err != nil {
		t.Fatalf("add golang: %v", err)
	}
	_, err = store.Add(ctx, memory.AddRequest{
		Summary: "wine club tier pricing",
		Body:    "bajka winery tier one two three pricing",
	})
	if err != nil {
		t.Fatalf("add wine: %v", err)
	}

	results, err := store.Search(ctx, memory.SearchRequest{Query: "wine", Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'wine'")
	}
	found := false
	for _, r := range results {
		if r.Summary == "wine club tier pricing" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected wine record in results; got: %+v", results)
	}
}

func TestSearchDetailedReportsFTSOnlyWithoutEmbedder(t *testing.T) {
	t.Parallel()
	store := testStore(t) // uses embedder.Disabled
	ctx := context.Background()

	_, err := store.Add(ctx, memory.AddRequest{
		Summary: "wine club tier pricing",
		Body:    "bajka winery tier one two three pricing",
	})
	if err != nil {
		t.Fatalf("add wine: %v", err)
	}

	response, err := store.SearchDetailed(ctx, memory.SearchRequest{Query: "wine", Limit: 5})
	if err != nil {
		t.Fatalf("search detailed: %v", err)
	}
	if response.Diagnostics.EmbedderUsed {
		t.Fatalf("EmbedderUsed = true, want false")
	}
	if response.Diagnostics.FTSHits == 0 {
		t.Fatalf("FTSHits = 0, want at least one; response=%+v", response)
	}
	if response.Diagnostics.VectorHits != 0 {
		t.Fatalf("VectorHits = %d, want 0", response.Diagnostics.VectorHits)
	}
}

func TestSearchDetailedReportsVectorLegWithEmbedder(t *testing.T) {
	t.Parallel()
	if os.Getenv("CGO_ENABLED") == "0" {
		t.Skip("skipping: cgo disabled")
	}
	dir := t.TempDir()
	store, err := memory.Open(memory.OpenConfig{
		ZvecPath:     filepath.Join(dir, "zvec"),
		EmbeddingDim: 768,
		LockTimeout:  2 * time.Second,
		FTSWeight:    0.5,
		VectorWeight: 0.5,
		Embedder:     keywordEmbedder{dim: 768},
	})
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	_, err = store.Add(ctx, memory.AddRequest{
		ID:      "diagnostics:arborist",
		Summary: "Tree doctor canopy inspection",
		Body:    "An arborist assessed a street tree with drought stress.",
	})
	if err != nil {
		t.Fatalf("add arborist: %v", err)
	}
	_, err = store.Add(ctx, memory.AddRequest{
		ID:      "diagnostics:fermentation",
		Summary: "Fermentation vessel sanitation",
		Body:    "Clean the wine tank before pitching yeast.",
	})
	if err != nil {
		t.Fatalf("add fermentation: %v", err)
	}

	response, err := store.SearchDetailed(ctx, memory.SearchRequest{Query: "tree doctor", Limit: 2})
	if err != nil {
		t.Fatalf("search detailed: %v", err)
	}
	if !response.Diagnostics.EmbedderUsed {
		t.Fatalf("EmbedderUsed = false, want true; diagnostics=%+v", response.Diagnostics)
	}
	if response.Diagnostics.QueryEmbeddingDimension != 768 {
		t.Fatalf("QueryEmbeddingDimension = %d, want 768", response.Diagnostics.QueryEmbeddingDimension)
	}
	if response.Diagnostics.VectorHits == 0 {
		t.Fatalf("VectorHits = 0, want vector leg participation; response=%+v", response)
	}
}
