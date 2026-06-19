package ingest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/ingeststate"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/summarizer"
)

type fakeTranscriptSummarizer struct {
	req summarizer.Request
	n   int
}

func (f *fakeTranscriptSummarizer) Summarize(_ context.Context, req summarizer.Request) (summarizer.Result, error) {
	f.req = req
	f.n++
	return summarizer.Result{Memories: []summarizer.GeneratedMemory{{
		Kind:    "preference",
		Summary: "Distilled preference",
		Body:    "User prefers durable memories to be distilled rather than copied.",
	}}}, nil
}

type failingTranscriptSummarizer struct {
	n int
}

func (f *failingTranscriptSummarizer) Summarize(context.Context, summarizer.Request) (summarizer.Result, error) {
	f.n++
	return summarizer.Result{}, errors.New("summarizer unavailable")
}

func TestScannerStoresSummarizedTranscriptMemoryWithSourceReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	transcript := filepath.Join(root, "session.jsonl")
	rawPrompt := "do not store this raw transcript line verbatim"
	data := `{"cwd":"/tmp/agent-memoryd","message":{"role":"user","content":"` + rawPrompt + `"}}` + "\n" +
		`{"message":{"role":"assistant","content":"ack"}}` + "\n"
	if err := os.WriteFile(transcript, []byte(data), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	modTime := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(transcript, modTime, modTime); err != nil {
		t.Fatalf("chtime transcript: %v", err)
	}

	store := newTestStore(t)
	if _, err := store.Add(ctx, memory.AddRequest{
		ID:      "existing",
		Project: "agent-memoryd",
		Summary: "Existing project memory",
		Body:    "Existing project memory body",
		Now:     modTime.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("add existing memory: %v", err)
	}
	fake := &fakeTranscriptSummarizer{}
	scanner := Scanner{
		Roots:              []string{root},
		Summarizer:         fake,
		MemoryContextLimit: 5,
	}

	count, err := scanner.Scan(ctx, store)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if !strings.Contains(fake.req.SourceMaterial, rawPrompt) {
		t.Fatal("expected raw transcript to be passed to summarizer")
	}
	if len(fake.req.ExistingMemories) != 1 || fake.req.ExistingMemories[0].ID != "existing" {
		t.Fatalf("existing context = %#v, want existing memory", fake.req.ExistingMemories)
	}
	records, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	var got memory.Record
	for _, record := range records {
		if record.Source == transcript {
			got = record
			break
		}
	}
	if got.ID == "" {
		t.Fatal("expected summarized transcript memory")
	}
	if strings.Contains(got.Body, rawPrompt) {
		t.Fatalf("stored body contains raw prompt: %q", got.Body)
	}
	if !strings.Contains(got.Body, "More detail: Transcript: "+transcript) {
		t.Fatalf("stored body missing transcript reference: %q", got.Body)
	}
}

func TestScannerStoresOpenCodeExportedSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	transcript := filepath.Join(root, "opencode-session.json")
	rawPrompt := "remember OpenCode exported sessions"
	data := `{
		"info":{"id":"ses_test","directory":"/tmp/agent-memoryd","time":{"updated":1781710400000}},
		"messages":[
			{"info":{"role":"user","path":{"cwd":"/tmp/agent-memoryd"}},"parts":[{"type":"text","text":"` + rawPrompt + `"}]},
			{"info":{"role":"assistant","path":{"cwd":"/tmp/agent-memoryd"}},"parts":[{"type":"text","text":"ack"},{"type":"tool","tool":"bash"}]}
		]
	}`
	if err := os.WriteFile(transcript, []byte(data), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	modTime := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(transcript, modTime, modTime); err != nil {
		t.Fatalf("chtime transcript: %v", err)
	}

	store := newTestStore(t)
	fake := &fakeTranscriptSummarizer{}
	scanner := Scanner{
		Roots:      []string{root},
		Summarizer: fake,
	}

	count, err := scanner.Scan(ctx, store)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if fake.req.Source != transcript {
		t.Fatalf("source = %q, want %q", fake.req.Source, transcript)
	}
	if fake.req.Project != "agent-memoryd" {
		t.Fatalf("project = %q, want agent-memoryd", fake.req.Project)
	}
	if !strings.Contains(fake.req.SourceMaterial, rawPrompt) {
		t.Fatal("expected OpenCode export to be passed to summarizer")
	}
}

func TestScannerUsesOpenCodeCLIForOpenCodeRoot(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir opencode root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "opencode.db"), nil, 0o644); err != nil {
		t.Fatalf("write opencode db marker: %v", err)
	}
	fixture := filepath.Join(t.TempDir(), "export.json")
	if err := os.WriteFile(fixture, []byte(`{
		"info":{"id":"ses_cli","directory":"/tmp/agent-memoryd","time":{"updated":1781710400000}},
		"messages":[{"info":{"role":"user"},"parts":[{"type":"text","text":"remember CLI exported sessions"}]}]
	}`), 0o644); err != nil {
		t.Fatalf("write export fixture: %v", err)
	}
	bin := t.TempDir()
	script := filepath.Join(bin, "opencode")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
if [ "$1" = "session" ] && [ "$2" = "list" ]; then
  printf 'Session ID                      Title\n'
  printf 'ses_cli                        Test session\n'
  exit 0
fi
if [ "$1" = "export" ] && [ "$2" = "ses_cli" ]; then
  cat "$OPENCODE_EXPORT_FIXTURE"
  exit 0
fi
exit 1
`), 0o755); err != nil {
		t.Fatalf("write opencode script: %v", err)
	}
	t.Setenv("OPENCODE_EXPORT_FIXTURE", fixture)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	store := newTestStore(t)
	state := &ingeststate.State{Inputs: map[string]ingeststate.Input{}}
	fake := &fakeTranscriptSummarizer{}
	scanner := Scanner{
		Roots:      []string{root},
		Summarizer: fake,
		State:      state,
		Now:        time.Date(2026, 6, 16, 13, 0, 0, 0, time.UTC),
	}

	first, err := scanner.Scan(ctx, store)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	second, err := scanner.Scan(ctx, store)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if first != 1 || second != 0 {
		t.Fatalf("scan counts = %d, %d; want 1, 0", first, second)
	}
	if fake.req.Source != "opencode:ses_cli" {
		t.Fatalf("source = %q, want opencode:ses_cli", fake.req.Source)
	}
	if state.Inputs["opencode:ses_cli"].Status != "processed" {
		t.Fatalf("opencode input state = %#v, want processed", state.Inputs["opencode:ses_cli"])
	}
}

func TestScannerSkipsTranscriptsOlderThanCutoff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	transcript := filepath.Join(root, "old.jsonl")
	data := `{"cwd":"/tmp/agent-memoryd","message":{"role":"user","content":"old prompt"}}` + "\n"
	if err := os.WriteFile(transcript, []byte(data), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	modTime := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(transcript, modTime, modTime); err != nil {
		t.Fatalf("chtime transcript: %v", err)
	}

	store := newTestStore(t)
	fake := &fakeTranscriptSummarizer{}
	scanner := Scanner{
		Roots:      []string{root},
		NotBefore:  modTime.Add(time.Second),
		Summarizer: fake,
	}

	count, err := scanner.Scan(ctx, store)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if fake.req.Producer != "" {
		t.Fatalf("summarizer was called for old transcript: %#v", fake.req)
	}
}

func TestScannerStateSkipsProcessedTranscriptFingerprint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	transcript := filepath.Join(root, "session.jsonl")
	data := `{"cwd":"/tmp/agent-memoryd","message":{"role":"user","content":"remember this once"}}` + "\n"
	if err := os.WriteFile(transcript, []byte(data), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	modTime := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(transcript, modTime, modTime); err != nil {
		t.Fatalf("chtime transcript: %v", err)
	}
	store := newTestStore(t)
	state := &ingeststate.State{Inputs: map[string]ingeststate.Input{}}
	fake := &fakeTranscriptSummarizer{}
	scanner := Scanner{
		Roots:      []string{root},
		Summarizer: fake,
		State:      state,
		Now:        modTime.Add(time.Hour),
	}

	first, err := scanner.Scan(ctx, store)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	second, err := scanner.Scan(ctx, store)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if first != 1 || second != 0 {
		t.Fatalf("scan counts = %d, %d; want 1, 0", first, second)
	}
	if fake.n != 1 {
		t.Fatalf("summarizer calls = %d, want 1", fake.n)
	}
}

func TestScannerStateQuarantinesRepeatedTranscriptFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	transcript := filepath.Join(root, "session.jsonl")
	data := `{"cwd":"/tmp/agent-memoryd","message":{"role":"user","content":"bad transcript"}}` + "\n"
	if err := os.WriteFile(transcript, []byte(data), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	modTime := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(transcript, modTime, modTime); err != nil {
		t.Fatalf("chtime transcript: %v", err)
	}
	store := newTestStore(t)
	state := &ingeststate.State{Inputs: map[string]ingeststate.Input{}}
	fake := &failingTranscriptSummarizer{}
	scanner := Scanner{
		Roots:      []string{root},
		Summarizer: fake,
		State:      state,
	}
	for i := 0; i < 4; i++ {
		scanner.Now = modTime.Add(time.Duration(i) * 10 * time.Minute)
		if _, err := scanner.Scan(ctx, store); err != nil {
			t.Fatalf("scan %d: %v", i, err)
		}
	}
	if fake.n != 3 {
		t.Fatalf("summarizer calls = %d, want 3 before quarantine", fake.n)
	}
	input := state.Inputs["transcript:"+transcript]
	if input.Status != "quarantined" {
		t.Fatalf("input status = %q, want quarantined", input.Status)
	}
}
