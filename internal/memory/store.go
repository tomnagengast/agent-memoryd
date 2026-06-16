package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	ErrEmptyBody = errors.New("memory body is empty")
	ErrNotFound  = errors.New("memory not found")
)

type Store struct {
	mu   sync.Mutex
	path string
}

type SearchRequest struct {
	Query   string
	Kind    string
	Project string
	Limit   int
}

type SearchResult struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Project string  `json:"project,omitempty"`
	Source  string  `json:"source,omitempty"`
	Summary string  `json:"summary"`
	Score   float64 `json:"score"`
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Add(ctx context.Context, req AddRequest) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	records, err := s.readLocked()
	if err != nil {
		return Record{}, err
	}
	byID := make(map[string]Record, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}

	var record Record
	if existing, ok := byID[strings.TrimSpace(req.ID)]; ok {
		record, err = existing.Updated(req)
	} else {
		record, err = NewRecord(req)
	}
	if err != nil {
		return Record{}, err
	}
	byID[record.ID] = record
	return record, s.writeLocked(mapValues(byID))
}

func (s *Store) Get(ctx context.Context, id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	records, err := s.readLocked()
	if err != nil {
		return Record{}, err
	}
	for _, record := range records {
		if record.ID == id {
			return record, nil
		}
	}
	return Record{}, ErrNotFound
}

func (s *Store) Forget(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	records, err := s.readLocked()
	if err != nil {
		return err
	}
	next := records[:0]
	found := false
	for _, record := range records {
		if record.ID == id {
			found = true
			continue
		}
		next = append(next, record)
	}
	if !found {
		return ErrNotFound
	}
	return s.writeLocked(next)
}

func (s *Store) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
	records, err := s.readLocked()
	if err != nil {
		return nil, err
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

func (s *Store) readLocked() ([]Record, error) {
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	defer file.Close()

	var records []Record
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("decode memory record: %w", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read memory store: %w", err)
	}
	return records, nil
}

func (s *Store) writeLocked(records []Record) error {
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.Before(records[j].CreatedAt)
	})
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create memory store dir: %w", err)
	}
	tmp := s.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create memory store temp file: %w", err)
	}
	enc := json.NewEncoder(file)
	for _, record := range records {
		if err := enc.Encode(record); err != nil {
			_ = file.Close()
			return fmt.Errorf("encode memory record: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close memory store temp file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace memory store: %w", err)
	}
	return nil
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

func mapValues(records map[string]Record) []Record {
	values := make([]Record, 0, len(records))
	for _, record := range records {
		values = append(values, record)
	}
	return values
}
