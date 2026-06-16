package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/summarizer"
)

type fakeTranscriptSummarizer struct {
	req summarizer.Request
}

func (f *fakeTranscriptSummarizer) Summarize(_ context.Context, req summarizer.Request) (summarizer.Result, error) {
	f.req = req
	return summarizer.Result{Memories: []summarizer.GeneratedMemory{{
		Kind:    "preference",
		Summary: "Distilled preference",
		Body:    "User prefers durable memories to be distilled rather than copied.",
	}}}, nil
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

	store := memory.NewStore(filepath.Join(t.TempDir(), "memories.jsonl"))
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

	store := memory.NewStore(filepath.Join(t.TempDir(), "memories.jsonl"))
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
