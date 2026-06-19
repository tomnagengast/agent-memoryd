package spool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/ingeststate"
	"github.com/tomnagengast/agent-memoryd/internal/summarizer"
)

type fakeGitSummarizer struct {
	req summarizer.Request
}

func (f *fakeGitSummarizer) Summarize(_ context.Context, req summarizer.Request) (summarizer.Result, error) {
	f.req = req
	return summarizer.Result{Memories: []summarizer.GeneratedMemory{{
		Kind:    "git-summary",
		Summary: "Distilled git memory",
		Body:    "Commit changed how generated memories are produced.",
	}}}, nil
}

type failingGitSummarizer struct{}

func (f failingGitSummarizer) Summarize(context.Context, summarizer.Request) (summarizer.Result, error) {
	return summarizer.Result{}, errors.New("summarizer unavailable")
}

func TestProcessGitStoresSummarizedMemoryWithCommitReference(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	spoolDir := t.TempDir()
	store := newTestStore(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}
	event := GitEvent{
		Repo:      repo,
		SHA:       "abc123",
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	}
	if _, err := EnqueueGit(spoolDir, event); err != nil {
		t.Fatalf("enqueue git: %v", err)
	}
	fake := &fakeGitSummarizer{}

	state := &ingeststate.State{Inputs: map[string]ingeststate.Input{}}
	count, err := ProcessGit(ctx, spoolDir, store, fake, 5, state)
	if err != nil {
		t.Fatalf("process git: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if !strings.Contains(fake.req.SourceMaterial, "Commit: abc123") {
		t.Fatalf("source material missing commit: %q", fake.req.SourceMaterial)
	}
	records, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if strings.Contains(records[0].Body, "Git summary:") {
		t.Fatalf("stored body contains raw git summary: %q", records[0].Body)
	}
	if !strings.Contains(records[0].Body, "More detail: Commit: abc123") || !strings.Contains(records[0].Body, "Repo: "+repo) {
		t.Fatalf("stored body missing progressive disclosure reference: %q", records[0].Body)
	}
	entries, err := os.ReadDir(spoolDir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("spool entries after processing = %d, want 0", len(entries))
	}
}

func TestProcessGitQuarantinesRepeatedFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	spoolDir := t.TempDir()
	store := newTestStore(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}
	if _, err := EnqueueGit(spoolDir, GitEvent{
		Repo:      repo,
		SHA:       "abc123",
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("enqueue git: %v", err)
	}
	state := &ingeststate.State{Inputs: map[string]ingeststate.Input{}}
	for i := 0; i < 3; i++ {
		if _, err := ProcessGit(ctx, spoolDir, store, failingGitSummarizer{}, 5, state); err != nil {
			t.Fatalf("process git %d: %v", i, err)
		}
		for key, input := range state.Inputs {
			if input.Status == "failed" {
				input.NextAttemptAt = nil
				state.Inputs[key] = input
			}
		}
	}
	entries, err := os.ReadDir(spoolDir)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() || entries[0].Name() != "failed" {
		t.Fatalf("spool entries = %#v, want failed dir only", entries)
	}
	failedEntries, err := os.ReadDir(filepath.Join(spoolDir, "failed"))
	if err != nil {
		t.Fatalf("read failed dir: %v", err)
	}
	if len(failedEntries) != 1 {
		t.Fatalf("failed entries = %d, want 1", len(failedEntries))
	}
	var got ingeststate.Input
	for _, input := range state.Inputs {
		got = input
	}
	if got.Status != "quarantined" {
		t.Fatalf("state status = %q, want quarantined", got.Status)
	}
}
