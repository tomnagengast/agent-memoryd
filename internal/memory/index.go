package memory

import "context"

type Index interface {
	Name() string
	Search(context.Context, []Record, SearchRequest) ([]SearchResult, error)
	Upsert(context.Context, Record) error
	Delete(context.Context, string) error
	Rebuild(context.Context, []Record) error
}

type LexicalIndex struct{}

func (LexicalIndex) Name() string {
	return "lexical"
}

func (LexicalIndex) Search(_ context.Context, records []Record, req SearchRequest) ([]SearchResult, error) {
	return SearchLexical(records, req)
}

func (LexicalIndex) Upsert(context.Context, Record) error {
	return nil
}

func (LexicalIndex) Delete(context.Context, string) error {
	return nil
}

func (LexicalIndex) Rebuild(context.Context, []Record) error {
	return nil
}
