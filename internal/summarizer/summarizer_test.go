package summarizer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

func TestParseResultFiltersEmptyMemoriesAndAddsSummaries(t *testing.T) {
	t.Parallel()
	result, err := ParseResult(`{"memories":[{"kind":"preference","body":"Prefer concise answers."},{"body":""}]}`)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(result.Memories) != 1 {
		t.Fatalf("len(result.Memories) = %d, want 1", len(result.Memories))
	}
	if result.Memories[0].Summary == "" {
		t.Fatal("expected missing summary to be derived")
	}
}

func TestExistingMemoryRefsReturnsRecentProjectScopedSummaries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newTestStore(t)
	_, err := store.Add(ctx, memory.AddRequest{
		ID:      "old",
		Project: "agent-memoryd",
		Summary: "old summary",
		Body:    "old body",
		Now:     time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("add old memory: %v", err)
	}
	_, err = store.Add(ctx, memory.AddRequest{
		ID:      "new",
		Project: "agent-memoryd",
		Summary: "new summary",
		Body:    "new body",
		Now:     time.Date(2026, 6, 16, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("add new memory: %v", err)
	}
	_, err = store.Add(ctx, memory.AddRequest{
		ID:      "other",
		Project: "other",
		Summary: "other summary",
		Body:    "other body",
		Now:     time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("add other memory: %v", err)
	}

	refs, err := ExistingMemoryRefs(ctx, store, "agent-memoryd", 1)
	if err != nil {
		t.Fatalf("existing memory refs: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "new" || refs[0].Summary != "new summary" {
		t.Fatalf("refs = %#v, want newest project-scoped memory", refs)
	}
}

func TestCommandAgentRedactsSubprocessOutputOnFailure(t *testing.T) {
	t.Parallel()
	agent := CommandAgent{
		Command: []string{"sh", "-c", "cat >/dev/null; printf 'raw transcript secret' >&2; exit 7"},
	}

	_, err := agent.Summarize(context.Background(), Request{
		Producer:       "transcript",
		Project:        "agent-memoryd",
		Source:         "source",
		SourceMaterial: "raw source secret",
	})
	if err == nil {
		t.Fatal("Summarize returned nil error")
	}
	text := err.Error()
	for _, leaked := range []string{"raw transcript secret", "raw source secret"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("error leaked %q: %v", leaked, err)
		}
	}
	if !strings.Contains(text, "subprocess output redacted") {
		t.Fatalf("error missing redaction marker: %v", err)
	}
}
