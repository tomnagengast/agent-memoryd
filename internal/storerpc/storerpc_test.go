// Package storerpc_test exercises the server/client round-trip using an
// in-memory fake memory.API so cgo/zvec is never needed.
package storerpc_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomnagengast/agent-memoryd/internal/config"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/storerpc"
)

// shortDir returns a short temporary directory path that stays under the
// 104-byte macOS Unix socket path limit.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "srpc")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// fakeAPI is a minimal in-memory implementation of memory.API for testing.
type fakeAPI struct {
	records map[string]memory.Record
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{records: make(map[string]memory.Record)}
}

func (f *fakeAPI) Add(_ context.Context, req memory.AddRequest) (memory.Record, error) {
	if req.Body == "" {
		return memory.Record{}, memory.ErrEmptyBody
	}
	id := req.ID
	if id == "" {
		id = "gen-" + req.Body[:min(8, len(req.Body))]
	}
	r := memory.Record{
		ID:      id,
		Kind:    req.Kind,
		Project: req.Project,
		Source:  req.Source,
		Summary: req.Summary,
		Body:    req.Body,
	}
	f.records[id] = r
	return r, nil
}

func (f *fakeAPI) Get(_ context.Context, id string) (memory.Record, error) {
	r, ok := f.records[id]
	if !ok {
		return memory.Record{}, memory.ErrNotFound
	}
	return r, nil
}

func (f *fakeAPI) Search(_ context.Context, _ memory.SearchRequest) ([]memory.SearchResult, error) {
	return nil, nil
}

func (f *fakeAPI) SearchDetailed(_ context.Context, _ memory.SearchRequest) (memory.SearchResponse, error) {
	return memory.SearchResponse{
		Results: []memory.SearchResult{{
			ID:      "search-001",
			Kind:    "fact",
			Summary: "diagnostic search result",
			Score:   1,
		}},
		Diagnostics: memory.SearchDiagnostics{
			EmbedderUsed:            true,
			FTSHits:                 1,
			VectorHits:              1,
			QueryEmbeddingDimension: 3,
		},
	}, nil
}

func (f *fakeAPI) Forget(_ context.Context, id string) error {
	if _, ok := f.records[id]; !ok {
		return memory.ErrNotFound
	}
	delete(f.records, id)
	return nil
}

func (f *fakeAPI) List(_ context.Context) ([]memory.Record, error) {
	out := make([]memory.Record, 0, len(f.records))
	for _, r := range f.records {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeAPI) Status(_ context.Context) (memory.Status, error) {
	return memory.Status{Path: "/fake", Backend: "fake", MemoryCount: len(f.records)}, nil
}

func (f *fakeAPI) Backfill(_ context.Context) (int, error) {
	return 0, nil
}

func (f *fakeAPI) Optimize(_ context.Context) error { return nil }

func (f *fakeAPI) Close() error { return nil }

// startTestServer starts a server on a temp unix socket and returns the config
// (pointing at the temp dir) plus a cancel func to stop the server.
func startTestServer(t *testing.T, api memory.API) (config.Config, context.CancelFunc) {
	t.Helper()
	// Use /tmp with a short name to stay under macOS's 104-byte socket path limit.
	dir := shortDir(t)
	cfg := config.Config{Root: dir}

	srv := storerpc.NewServer(api)
	ln, err := srv.Listen(cfg)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx, ln)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
		os.Remove(filepath.Join(dir, "memoryd.sock"))
	})

	return cfg, cancel
}

func TestRoundTrip_AddAndGet(t *testing.T) {
	fake := newFakeAPI()
	cfg, _ := startTestServer(t, fake)

	client := storerpc.NewClient(cfg)

	// Add a record.
	record, err := client.Add(context.Background(), memory.AddRequest{
		ID:   "test-001",
		Kind: "fact",
		Body: "hello world",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if record.ID != "test-001" {
		t.Errorf("Add returned ID %q, want test-001", record.ID)
	}
	if record.Body != "hello world" {
		t.Errorf("Add returned Body %q, want hello world", record.Body)
	}

	// Get it back.
	got, err := client.Get(context.Background(), "test-001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "test-001" {
		t.Errorf("Get returned ID %q, want test-001", got.ID)
	}
}

func TestRoundTrip_NotFoundErrorMapping(t *testing.T) {
	fake := newFakeAPI()
	cfg, _ := startTestServer(t, fake)

	client := storerpc.NewClient(cfg)

	_, err := client.Get(context.Background(), "nonexistent")
	if !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRoundTrip_SearchDetailed(t *testing.T) {
	fake := newFakeAPI()
	cfg, _ := startTestServer(t, fake)

	client := storerpc.NewClient(cfg)

	response, err := client.SearchDetailed(context.Background(), memory.SearchRequest{Query: "diagnostics"})
	if err != nil {
		t.Fatalf("SearchDetailed: %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].ID != "search-001" {
		t.Fatalf("results = %+v, want search-001", response.Results)
	}
	if !response.Diagnostics.EmbedderUsed || response.Diagnostics.FTSHits != 1 || response.Diagnostics.VectorHits != 1 || response.Diagnostics.QueryEmbeddingDimension != 3 {
		t.Fatalf("diagnostics = %+v, want detailed counts", response.Diagnostics)
	}
}

func TestRoundTrip_EmptyBodyErrorMapping(t *testing.T) {
	fake := newFakeAPI()
	cfg, _ := startTestServer(t, fake)

	client := storerpc.NewClient(cfg)

	_, err := client.Add(context.Background(), memory.AddRequest{Body: ""})
	if !errors.Is(err, memory.ErrEmptyBody) {
		t.Fatalf("expected ErrEmptyBody, got %v", err)
	}
}

func TestProbe(t *testing.T) {
	dir := shortDir(t)
	cfg := config.Config{Root: dir}

	// No daemon yet; Probe should return false.
	if storerpc.Probe(cfg) {
		t.Fatal("Probe returned true with no listener")
	}

	// Start a listener manually.
	ln, err := net.Listen("unix", storerpc.SocketPath(cfg))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	if !storerpc.Probe(cfg) {
		t.Fatal("Probe returned false with an active listener")
	}
}

func TestRoundTrip_Optimize(t *testing.T) {
	fake := newFakeAPI()
	cfg, _ := startTestServer(t, fake)

	client := storerpc.NewClient(cfg)

	if err := client.Optimize(context.Background()); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
