package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/config"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/storerpc"
	"github.com/tomnagengast/agent-memoryd/internal/summarizer"
)

func TestRunTopLevelHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		t.Run(arg, func(t *testing.T) {
			out, err := captureStdout(func() error {
				return Run([]string{arg})
			})
			if err != nil {
				t.Fatalf("Run(%q) returned error: %v", arg, err)
			}
			for _, want := range []string{
				"Local memory daemon for coding agents.",
				"Usage:",
				"Available Commands:",
				"mcp",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("help output missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestRunVersion(t *testing.T) {
	for _, arg := range []string{"-v", "--version"} {
		t.Run(arg, func(t *testing.T) {
			out, err := captureStdout(func() error {
				return Run([]string{arg})
			})
			if err != nil {
				t.Fatalf("Run(%q) returned error: %v", arg, err)
			}
			if !strings.Contains(out, "memoryd") {
				t.Fatalf("version output missing binary name:\n%s", out)
			}
		})
	}
}

func TestRunUnknownCommandMentionsHelp(t *testing.T) {
	err := Run([]string{"nope"})
	if err == nil {
		t.Fatal("Run returned nil error for unknown command")
	}
	if !strings.Contains(err.Error(), "memoryd --help") {
		t.Fatalf("unknown command error did not mention help: %v", err)
	}
}

func TestRunArgumentErrorMentionsHelp(t *testing.T) {
	err := Run([]string{"add"})
	if err == nil {
		t.Fatal("Run returned nil error for missing add body")
	}
	if !strings.Contains(err.Error(), "memoryd --help") {
		t.Fatalf("argument error did not mention help: %v", err)
	}
}

func TestRunSubcommandHelp(t *testing.T) {
	out, err := captureStdout(func() error {
		return Run([]string{"add", "--help"})
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	for _, want := range []string{
		"Create or update a memory.",
		"Usage:",
		"memoryd add [flags] <body>",
		"--summary",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("subcommand help missing %q:\n%s", want, out)
		}
	}
}

type fakeReflectSummarizer struct {
	req summarizer.Request
}

func (f *fakeReflectSummarizer) Summarize(_ context.Context, req summarizer.Request) (summarizer.Result, error) {
	f.req = req
	return summarizer.Result{Memories: []summarizer.GeneratedMemory{{
		Kind:    "preference",
		Summary: "Remember reflection preference",
		Body:    "User wants a reflect tool to persist memories from the current session.",
	}}}, nil
}

func TestReflectSessionTextStoresSummarizedMemory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestStore(t)
	fake := &fakeReflectSummarizer{}
	in := reflectInput{
		Project: "agent-memoryd",
		CWD:     "/tmp/agent-memoryd",
		Source:  "session:test",
		Session: "raw current session content that should only go to the summarizer",
	}

	records, err := reflectSessionText(ctx, store, fake, in, 5)
	if err != nil {
		t.Fatalf("reflect session text: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if fake.req.Producer != "reflect" || !strings.Contains(fake.req.SourceMaterial, in.Session) {
		t.Fatalf("summarizer request = %#v, want reflect request with session material", fake.req)
	}
	if records[0].Kind != "preference" {
		t.Fatalf("record kind = %q, want preference", records[0].Kind)
	}
	if !strings.Contains(records[0].Body, "More detail: Session: session:test") {
		t.Fatalf("record body missing detail reference: %q", records[0].Body)
	}
	if strings.Contains(records[0].Body, in.Session) {
		t.Fatalf("record body contains raw session material: %q", records[0].Body)
	}
}

func TestLatestTranscriptReturnsNewestJSONL(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.jsonl")
	newPath := filepath.Join(root, "new.jsonl")
	for _, path := range []string{oldPath, newPath} {
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
	}
	oldTime := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtime old transcript: %v", err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatalf("chtime new transcript: %v", err)
	}

	got, err := latestTranscript([]string{root})
	if err != nil {
		t.Fatalf("latest transcript: %v", err)
	}
	if got != newPath {
		t.Fatalf("latest transcript = %q, want %q", got, newPath)
	}
}

func TestDialOrOpenRequiresDaemon(t *testing.T) {
	t.Setenv("AGENT_MEMORYD_HOME", shortSocketDir(t))

	_, store, err := dialOrOpen()
	if !errors.Is(err, errDaemonNotRunning) {
		t.Fatalf("dialOrOpen error = %v, want errDaemonNotRunning", err)
	}
	if store != nil {
		t.Fatalf("dialOrOpen store = %#v, want nil", store)
	}
}

func TestDialOrOpenUsesDaemonSocket(t *testing.T) {
	ctx := context.Background()
	root := shortSocketDir(t)
	t.Setenv("AGENT_MEMORYD_HOME", root)
	cfg := config.Config{Root: root}
	stop := startFakeStoreRPC(t, cfg, newFakeMemoryAPI())
	defer stop()

	if !storerpc.Probe(cfg) {
		t.Fatal("Probe returned false for fake daemon RPC server")
	}
	_, store, err := dialOrOpen()
	if err != nil {
		t.Fatalf("dialOrOpen: %v", err)
	}
	defer store.Close()
	record, err := store.Add(ctx, memory.AddRequest{ID: "test:one", Body: "hello from rpc"})
	if err != nil {
		t.Fatalf("store add: %v", err)
	}
	got, err := store.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("store get: %v", err)
	}
	if got.Body != "hello from rpc" {
		t.Fatalf("got body %q, want %q", got.Body, "hello from rpc")
	}
}

func captureStdout(fn func() error) (string, error) {
	original := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = write

	fnErr := fn()
	closeErr := write.Close()
	os.Stdout = original

	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, read)
	readErr := read.Close()

	switch {
	case fnErr != nil:
		return buf.String(), fnErr
	case closeErr != nil:
		return buf.String(), closeErr
	case copyErr != nil:
		return buf.String(), copyErr
	case readErr != nil:
		return buf.String(), readErr
	default:
		return buf.String(), nil
	}
}

type fakeMemoryAPI struct {
	records map[string]memory.Record
}

func newFakeMemoryAPI() *fakeMemoryAPI {
	return &fakeMemoryAPI{records: make(map[string]memory.Record)}
}

func (f *fakeMemoryAPI) Add(_ context.Context, req memory.AddRequest) (memory.Record, error) {
	record, err := memory.NewRecord(req)
	if err != nil {
		return memory.Record{}, err
	}
	f.records[record.ID] = record
	return record, nil
}

func (f *fakeMemoryAPI) Get(_ context.Context, id string) (memory.Record, error) {
	record, ok := f.records[id]
	if !ok {
		return memory.Record{}, memory.ErrNotFound
	}
	return record, nil
}

func (f *fakeMemoryAPI) Search(_ context.Context, _ memory.SearchRequest) ([]memory.SearchResult, error) {
	return nil, nil
}

func (f *fakeMemoryAPI) Forget(_ context.Context, id string) error {
	if _, ok := f.records[id]; !ok {
		return memory.ErrNotFound
	}
	delete(f.records, id)
	return nil
}

func (f *fakeMemoryAPI) List(_ context.Context) ([]memory.Record, error) {
	records := make([]memory.Record, 0, len(f.records))
	for _, record := range f.records {
		records = append(records, record)
	}
	return records, nil
}

func (f *fakeMemoryAPI) Status(_ context.Context) (memory.Status, error) {
	return memory.Status{Path: "/fake", Backend: "fake", MemoryCount: len(f.records)}, nil
}

func (f *fakeMemoryAPI) Backfill(_ context.Context) (int, error) {
	return 0, nil
}

func (f *fakeMemoryAPI) Optimize(_ context.Context) error {
	return nil
}

func (f *fakeMemoryAPI) Close() error {
	return nil
}

func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "amapp")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func startFakeStoreRPC(t *testing.T, cfg config.Config, api memory.API) func() {
	t.Helper()
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
	return func() {
		cancel()
		<-done
		os.Remove(storerpc.SocketPath(cfg))
	}
}
