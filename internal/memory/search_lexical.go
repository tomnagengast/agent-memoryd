package memory

import (
	"fmt"
	"sort"
	"strings"
)

// SearchLexical performs an in-memory keyword search over a slice of records.
// It is used by the explore package's test fake and as a fallback when no
// zvec store is available.
func SearchLexical(records []Record, req SearchRequest) ([]SearchResult, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("search query is empty")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}

	queryTokens := tokenSet(query)
	results := make([]SearchResult, 0, len(records))
	for _, record := range records {
		if req.Kind != "" && record.Kind != req.Kind {
			continue
		}
		if req.Project != "" && record.Project != req.Project {
			continue
		}
		score := lexicalScore(queryTokens, record)
		if score == 0 {
			continue
		}
		results = append(results, SearchResult{
			ID:      record.ID,
			Kind:    record.Kind,
			Project: record.Project,
			Source:  record.Source,
			Summary: record.Summary,
			Score:   score,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID < results[j].ID
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func tokenSet(text string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if token != "" {
			set[token] = struct{}{}
		}
	}
	return set
}

func lexicalScore(query map[string]struct{}, record Record) float64 {
	textTokens := tokenSet(record.Summary + " " + record.Body + " " + record.Kind + " " + record.Project)
	var hits int
	for token := range query {
		if _, ok := textTokens[token]; ok {
			hits++
		}
	}
	if hits == 0 {
		return 0
	}
	return float64(hits) / float64(len(query))
}
