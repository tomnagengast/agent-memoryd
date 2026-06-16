package explore

import (
	"context"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

type fakeStore struct {
	records []memory.Record
}

func (f fakeStore) List(context.Context) ([]memory.Record, error) {
	return append([]memory.Record(nil), f.records...), nil
}

func (f fakeStore) Search(_ context.Context, req memory.SearchRequest) ([]memory.SearchResult, error) {
	return memory.SearchLexical(f.records, req)
}

func (f fakeStore) Get(_ context.Context, id string) (memory.Record, error) {
	for _, record := range f.records {
		if record.ID == id {
			return record, nil
		}
	}
	return memory.Record{}, memory.ErrNotFound
}

func TestNewModelShowsRecentMemoriesForEmptySearch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	model, err := NewModel(context.Background(), fakeStore{records: []memory.Record{
		{ID: "old", Kind: "fact", Summary: "Old", Body: "old body", UpdatedAt: now.Add(-time.Hour)},
		{ID: "new", Kind: "fact", Summary: "New", Body: "new body", UpdatedAt: now},
	}}, Options{Limit: 10})
	if err != nil {
		t.Fatalf("new model: %v", err)
	}
	if len(model.items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(model.items))
	}
	if model.items[0].Record.ID != "new" {
		t.Fatalf("first item = %q, want newest record", model.items[0].Record.ID)
	}
}

func TestRefreshUsesSearchResultsWhenQueryIsSet(t *testing.T) {
	t.Parallel()
	model, err := NewModel(context.Background(), fakeStore{records: []memory.Record{
		{ID: "codex", Kind: "fact", Summary: "Codex memory", Body: "Use codex for summarization."},
		{ID: "claude", Kind: "fact", Summary: "Claude memory", Body: "Use claude for transcripts."},
	}}, Options{Limit: 10})
	if err != nil {
		t.Fatalf("new model: %v", err)
	}
	model.input.SetValue("codex")
	model.refresh()
	if len(model.items) != 1 {
		t.Fatalf("len(items) = %d, want one search result", len(model.items))
	}
	if model.items[0].Record.ID != "codex" {
		t.Fatalf("search item = %q, want codex", model.items[0].Record.ID)
	}
}

func TestMoveSelectionClampsToResults(t *testing.T) {
	t.Parallel()
	model, err := NewModel(context.Background(), fakeStore{records: []memory.Record{
		{ID: "a", Kind: "fact", Summary: "A", Body: "a"},
		{ID: "b", Kind: "fact", Summary: "B", Body: "b"},
	}}, Options{Limit: 10})
	if err != nil {
		t.Fatalf("new model: %v", err)
	}
	model.moveSelection(10)
	if model.selected != 1 {
		t.Fatalf("selected = %d, want last item", model.selected)
	}
	model.moveSelection(-10)
	if model.selected != 0 {
		t.Fatalf("selected = %d, want first item", model.selected)
	}
}
