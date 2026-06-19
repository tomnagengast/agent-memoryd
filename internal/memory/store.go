package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/embedder"
	zvec "github.com/zvec-ai/zvec-go"
)

var (
	ErrEmptyBody = errors.New("memory body is empty")
	ErrNotFound  = errors.New("memory not found")
	ErrDimension = errors.New("embedding dimension mismatch")
)

var (
	zvecInitOnce sync.Once
	zvecInitErr  error
)

func initializeZvec() error {
	zvecInitOnce.Do(func() {
		cfg := zvec.NewConfigData()
		if cfg == nil {
			zvecInitErr = fmt.Errorf("create zvec config")
			return
		}
		defer cfg.Destroy()
		if err := cfg.SetConsoleLog(zvec.LogLevelError); err != nil {
			zvecInitErr = fmt.Errorf("configure zvec logging: %w", err)
			return
		}
		if err := zvec.Initialize(cfg); err != nil && !zvec.IsAlreadyExists(err) {
			zvecInitErr = err
		}
	})
	return zvecInitErr
}

type Store struct {
	mu       sync.Mutex
	coll     *zvec.Collection
	path     string
	embedder embedder.Embedder
	weights  BlendWeights
	dim      int
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

type SearchResponse struct {
	Results     []SearchResult    `json:"results"`
	Diagnostics SearchDiagnostics `json:"diagnostics"`
}

type SearchDiagnostics struct {
	EmbedderUsed            bool `json:"embedder_used"`
	FTSHits                 int  `json:"fts_hits"`
	VectorHits              int  `json:"vector_hits"`
	QueryEmbeddingDimension int  `json:"query_embedding_dimension,omitempty"`
}

type OpenConfig struct {
	ZvecPath     string
	EmbeddingDim int
	LockTimeout  time.Duration
	FTSWeight    float64
	VectorWeight float64
	Embedder     embedder.Embedder
}

func Open(cfg OpenConfig) (*Store, error) {
	if err := initializeZvec(); err != nil {
		return nil, fmt.Errorf("initialize zvec: %w", err)
	}

	var coll *zvec.Collection
	var err error
	if _, statErr := os.Stat(cfg.ZvecPath); statErr == nil {
		coll, err = zvec.Open(cfg.ZvecPath, nil)
		if err == nil {
			// Flush immediately after opening an existing collection to ensure the
			// FTS index is fully loaded and available for queries. Without this,
			// FTS queries return 0 results when the collection is reopened within
			// the same process after records were previously written via Upsert+Flush.
			if flushErr := coll.Flush(); flushErr != nil {
				coll.Close()
				return nil, fmt.Errorf("post-open flush: %w", flushErr)
			}
		}
	} else {
		schema, schemaErr := newSchema(cfg.EmbeddingDim)
		if schemaErr != nil {
			return nil, schemaErr
		}
		defer schema.Destroy()
		coll, err = zvec.CreateAndOpen(cfg.ZvecPath, schema, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("open zvec collection: %w", err)
	}

	// First-run migration: import legacy memories.jsonl (if present and collection
	// is empty) into the zvec collection. Runs under zvec's exclusive directory lock.
	jsonlPath := filepath.Join(filepath.Dir(cfg.ZvecPath), "memories.jsonl")
	if _, migrErr := migrateIfNeeded(coll, jsonlPath); migrErr != nil {
		coll.Close()
		return nil, fmt.Errorf("migrate jsonl: %w", migrErr)
	}

	emb := cfg.Embedder
	if emb == nil {
		emb = embedder.Disabled{}
	}
	return &Store{
		coll:     coll,
		path:     cfg.ZvecPath,
		embedder: emb,
		weights:  BlendWeights{FTS: cfg.FTSWeight, Vector: cfg.VectorWeight},
		dim:      cfg.EmbeddingDim,
	}, nil
}

func (s *Store) Close() error {
	if s.coll != nil {
		return s.coll.Close()
	}
	return nil
}

// Optimize merges pending FTS index segments so that records written in this
// session are durable and visible to FTS queries in future sessions (i.e.
// after a process restart). It is cheap (~0.4 ms) when nothing has changed and
// ~200-400 ms when there are pending merges. It must be called before Close
// when writes need to survive a process restart.
func (s *Store) Optimize(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.coll.Optimize(); err != nil {
		return fmt.Errorf("optimize: %w", err)
	}
	return nil
}

func (s *Store) Add(ctx context.Context, req AddRequest) (Record, error) {
	// Normalize the caller-supplied ID to a form the zvec native lib accepts.
	// Colons are a common namespace separator but are rejected by the lib; replace
	// them with underscores before the ID is stamped into the Record.
	if req.ID != "" {
		req.ID = sanitizePK(req.ID)
	}
	record, err := NewRecord(req)
	if err != nil {
		return Record{}, err
	}
	// Embed OUTSIDE the lock: subprocess call should not hold s.mu.
	vec, embedErr := s.embedder.Embed(ctx, record.Summary+"\n"+record.Body)
	if embedErr != nil && !errors.Is(embedErr, embedder.ErrDisabled) {
		vec = nil // treat other errors as pending - vector will be backfilled
	}
	if vec != nil && len(vec) != s.dim {
		return Record{}, fmt.Errorf("%w: expected %d, got %d", ErrDimension, s.dim, len(vec))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, docErr := recordToDoc(record, vec)
	if docErr != nil {
		return Record{}, docErr
	}
	defer doc.Destroy()
	if _, upsertErr := s.coll.Upsert([]*zvec.Doc{doc}); upsertErr != nil {
		return Record{}, fmt.Errorf("upsert: %w", upsertErr)
	}
	if err := s.coll.Flush(); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Store) Get(ctx context.Context, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	docs, err := s.coll.Fetch([]string{sanitizePK(id)}, nil)
	if err != nil {
		return Record{}, fmt.Errorf("fetch: %w", err)
	}
	defer zvec.FreeDocs(docs)
	if len(docs) == 0 {
		return Record{}, ErrNotFound
	}
	return docToRecord(docs[0])
}

func (s *Store) Forget(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.coll.Delete([]string{sanitizePK(id)})
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if result.SuccessCount == 0 {
		return ErrNotFound
	}
	return s.coll.Flush()
}

func (s *Store) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	response, err := s.SearchDetailed(ctx, req)
	if err != nil {
		return nil, err
	}
	return response.Results, nil
}

func (s *Store) SearchDetailed(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if err := ctx.Err(); err != nil {
		return SearchResponse{}, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}

	filter := filterExpr(req)

	// Embed OUTSIDE the lock: subprocess call should not hold s.mu.
	queryVec, embedErr := s.embedder.Embed(ctx, req.Query)
	diagnostics := SearchDiagnostics{
		EmbedderUsed: embedErr == nil && len(queryVec) == s.dim,
	}
	if embedErr == nil && len(queryVec) > 0 {
		diagnostics.QueryEmbeddingDimension = len(queryVec)
	}

	s.mu.Lock()
	locked := true
	defer func() {
		if locked {
			s.mu.Unlock()
		}
	}()

	// FTS leg using zvec FTS API: NewFTS() + SetMatchString + query.SetFTS
	ftsQuery := zvec.NewSearchQuery()
	defer ftsQuery.Destroy()
	if err := ftsQuery.SetFieldName("summary"); err != nil {
		return SearchResponse{}, fmt.Errorf("set fts field name: %w", err)
	}
	fts := zvec.NewFTS()
	if err := fts.SetMatchString(req.Query); err != nil {
		fts.Destroy()
		return SearchResponse{}, fmt.Errorf("set fts match string: %w", err)
	}
	if err := ftsQuery.SetFTS(fts); err != nil {
		fts.Destroy()
		return SearchResponse{}, fmt.Errorf("set fts query: %w", err)
	}
	fts.Destroy()
	if err := ftsQuery.SetTopK(limit); err != nil {
		return SearchResponse{}, err
	}
	if filter != "" {
		if err := ftsQuery.SetFilter(filter); err != nil {
			return SearchResponse{}, err
		}
	}
	if err := ftsQuery.SetOutputFields([]string{"kind", "project", "source", "summary"}); err != nil {
		return SearchResponse{}, err
	}
	ftsDocs, err := s.coll.Query(ftsQuery)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("fts query: %w", err)
	}
	defer zvec.FreeDocs(ftsDocs)
	ftsResults, err := s.docsToLiveResultsLocked(ftsDocs)
	if err != nil {
		return SearchResponse{}, err
	}
	diagnostics.FTSHits = len(ftsResults)

	// Vector leg - skip if embedder disabled or fails
	var vecResults []SearchResult
	if embedErr == nil && len(queryVec) == s.dim {
		vecQuery := zvec.NewSearchQuery()
		defer vecQuery.Destroy()
		if err := vecQuery.SetFieldName("embedding"); err == nil {
			if err := vecQuery.SetQueryVector(queryVec); err == nil {
				if err := vecQuery.SetTopK(limit); err == nil {
					if filter != "" {
						vecQuery.SetFilter(filter) //nolint:errcheck
					}
					vecQuery.SetOutputFields([]string{"kind", "project", "source", "summary"}) //nolint:errcheck
					vecDocs, err := s.coll.Query(vecQuery)
					if err == nil {
						defer zvec.FreeDocs(vecDocs)
						if liveResults, liveErr := s.docsToLiveResultsLocked(vecDocs); liveErr == nil {
							vecResults = liveResults
						}
					}
				}
			}
		}
	}
	s.mu.Unlock()
	locked = false

	if filter != "" && embedErr == nil && len(queryVec) == s.dim {
		bruteResults, err := s.searchFilteredByEmbedding(ctx, req, queryVec, limit)
		if err == nil && len(bruteResults) > 0 {
			vecResults = bruteResults
		}
	}
	diagnostics.VectorHits = len(vecResults)

	// Blend results
	if len(vecResults) == 0 {
		return SearchResponse{Results: ftsResults, Diagnostics: diagnostics}, nil
	}
	return SearchResponse{
		Results:     blend(ftsResults, vecResults, s.weights, limit),
		Diagnostics: diagnostics,
	}, nil
}

func (s *Store) searchFilteredByEmbedding(ctx context.Context, req SearchRequest, queryVec []float32, limit int) ([]SearchResult, error) {
	records, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0)
	for _, record := range records {
		if req.Project != "" && record.Project != req.Project {
			continue
		}
		if req.Kind != "" && record.Kind != req.Kind {
			continue
		}
		vec, embedErr := s.embedder.Embed(ctx, record.Summary+"\n"+record.Body)
		if embedErr != nil || len(vec) != len(queryVec) {
			continue
		}
		results = append(results, SearchResult{
			ID:      record.ID,
			Kind:    record.Kind,
			Project: record.Project,
			Source:  record.Source,
			Summary: record.Summary,
			Score:   cosineDistance(queryVec, vec),
		})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].ID < results[j].ID
		}
		return results[i].Score < results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func cosineDistance(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		normA += af * af
		normB += bf * bf
	}
	if normA == 0 || normB == 0 {
		return 1
	}
	cosine := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	return 1 - cosine
}

func (s *Store) List(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stats, err := s.coll.GetStats()
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}
	if stats.DocCount == 0 {
		return nil, nil
	}
	// Use FTS match on the _tag field (all docs carry _tag="mem") to enumerate all documents.
	// A Lucene wildcard "*" does not work with the standard tokenizer; matching the constant
	// marker "mem" reliably returns every record regardless of its content.
	query := zvec.NewSearchQuery()
	defer query.Destroy()
	if err := query.SetFieldName("_tag"); err != nil {
		return nil, fmt.Errorf("list set field name: %w", err)
	}
	fts := zvec.NewFTS()
	if err := fts.SetMatchString("mem"); err != nil {
		fts.Destroy()
		return nil, fmt.Errorf("list set match string: %w", err)
	}
	if err := query.SetFTS(fts); err != nil {
		fts.Destroy()
		return nil, fmt.Errorf("list set fts: %w", err)
	}
	fts.Destroy()
	topK := int(stats.DocCount)
	if topK < 1 {
		topK = 1
	}
	if topK > 10000 {
		topK = 10000
	}
	if err := query.SetTopK(topK); err != nil {
		return nil, fmt.Errorf("list set topk: %w", err)
	}
	docs, err := s.coll.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list query: %w", err)
	}
	defer zvec.FreeDocs(docs)
	liveDocs, err := s.fetchLiveDocsLocked(docs)
	if err != nil {
		return nil, err
	}
	defer zvec.FreeDocs(liveDocs)
	records := make([]Record, 0, len(docs))
	for _, doc := range liveDocs {
		r, err := docToRecord(doc)
		if err != nil {
			continue
		}
		records = append(records, r)
	}
	return records, nil
}

func (s *Store) Status(ctx context.Context) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}

	// Get stats under lock.
	s.mu.Lock()
	stats, err := s.coll.GetStats()
	s.mu.Unlock()
	if err != nil {
		return Status{}, fmt.Errorf("get stats: %w", err)
	}

	// Count pending embeddings (enumerates docs under lock).
	pending, err := s.pendingCount(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("pending count: %w", err)
	}

	// Probe embedder OUTSIDE the lock: subprocess call must not hold s.mu.
	var probe EmbedderProbe
	testVec, perr := s.embedder.Embed(ctx, "probe")
	if perr == nil {
		probe = EmbedderProbe{Configured: true, OK: true, Dimension: len(testVec)}
	} else if errors.Is(perr, embedder.ErrDisabled) {
		probe = EmbedderProbe{Configured: false}
	} else {
		probe = EmbedderProbe{Configured: true, OK: false}
	}

	return Status{
		Path:             s.path,
		Backend:          "zvec",
		MemoryCount:      int(stats.DocCount),
		PendingEmbedding: pending,
		Embedder:         probe,
	}, nil
}

// pendingCount returns the number of documents whose embedding has not been computed.
// It enumerates PKs via the _tag FTS query, fetches without IncludeVector, and checks
// the embedding_dim scalar field (0 = pending, >0 = embedded). This avoids using
// zvec's nullable-vector introspection, which is unreliable on Fetch results without
// IncludeVector, and avoids Fetch with IncludeVector, which errors on null embeddings.
func (s *Store) pendingCount(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	pks, err := s.allPKsLocked()
	if err != nil {
		return 0, fmt.Errorf("pending all pks: %w", err)
	}
	if len(pks) == 0 {
		return 0, nil
	}
	fetchDocs, err := s.coll.Fetch(pks, nil)
	if err != nil {
		return 0, fmt.Errorf("pending fetch: %w", err)
	}
	defer zvec.FreeDocs(fetchDocs)
	count := 0
	for _, doc := range fetchDocs {
		dim, _ := doc.GetInt32Field("embedding_dim")
		if dim == 0 {
			count++
		}
	}
	return count, nil
}

// allPKsLocked returns all document PKs by running the _tag FTS enumeration query.
// Caller must hold s.mu.
func (s *Store) allPKsLocked() ([]string, error) {
	stats, err := s.coll.GetStats()
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}
	if stats.DocCount == 0 {
		return nil, nil
	}
	query := zvec.NewSearchQuery()
	defer query.Destroy()
	if err := query.SetFieldName("_tag"); err != nil {
		return nil, fmt.Errorf("set field name: %w", err)
	}
	fts := zvec.NewFTS()
	if err := fts.SetMatchString("mem"); err != nil {
		fts.Destroy()
		return nil, fmt.Errorf("set match string: %w", err)
	}
	if err := query.SetFTS(fts); err != nil {
		fts.Destroy()
		return nil, fmt.Errorf("set fts: %w", err)
	}
	fts.Destroy()
	topK := int(stats.DocCount)
	if topK < 1 {
		topK = 1
	}
	if topK > 10000 {
		topK = 10000
	}
	if err := query.SetTopK(topK); err != nil {
		return nil, fmt.Errorf("set topk: %w", err)
	}
	docs, err := s.coll.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer zvec.FreeDocs(docs)
	pks := make([]string, 0, len(docs))
	for _, doc := range docs {
		if pk := doc.GetPK(); pk != "" {
			pks = append(pks, pk)
		}
	}
	return pks, nil
}

// pendingItem holds the PK + text of a doc that needs its embedding filled.
type pendingItem struct {
	pk      string
	summary string
	body    string
}

func (s *Store) Backfill(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	// Enumerate all PKs via FTS query, then Fetch (no IncludeVector) to check
	// which docs have null embeddings via IsFieldNull("embedding"). Two-step because
	// FTS result batches never include vector columns, and Fetch with IncludeVector
	// crashes internally on docs with null embeddings.
	s.mu.Lock()
	pks, pksErr := s.allPKsLocked()
	if pksErr != nil {
		s.mu.Unlock()
		return 0, fmt.Errorf("backfill all pks: %w", pksErr)
	}
	var pending []pendingItem
	if len(pks) > 0 {
		fetchDocs, fetchErr := s.coll.Fetch(pks, nil)
		if fetchErr != nil {
			s.mu.Unlock()
			return 0, fmt.Errorf("backfill fetch: %w", fetchErr)
		}
		for _, doc := range fetchDocs {
			// embedding_dim == 0 means the embedding has not been computed yet.
			dim, _ := doc.GetInt32Field("embedding_dim")
			if dim == 0 {
				summary, _ := doc.GetStringField("summary")
				body, _ := doc.GetStringField("body")
				pending = append(pending, pendingItem{
					pk:      doc.GetPK(),
					summary: summary,
					body:    body,
				})
			}
		}
		zvec.FreeDocs(fetchDocs)
	}
	s.mu.Unlock() // RELEASE before any Embed subprocess calls.

	embedded := 0
	for _, item := range pending {
		if err := ctx.Err(); err != nil {
			break
		}
		text := item.summary + "\n" + item.body
		// Embed OUTSIDE the lock.
		vec, embedErr := s.embedder.Embed(ctx, text)
		if embedErr != nil || len(vec) != s.dim {
			// Skip rows we can't embed (disabled, error, or wrong dim).
			continue
		}
		// Re-fetch the doc fresh, update with vector, upsert - under lock.
		s.mu.Lock()
		freshDocs, fetchErr := s.coll.Fetch([]string{item.pk}, nil)
		if fetchErr != nil || len(freshDocs) == 0 {
			s.mu.Unlock()
			continue
		}
		record, recErr := docToRecord(freshDocs[0])
		zvec.FreeDocs(freshDocs)
		if recErr != nil {
			s.mu.Unlock()
			continue
		}
		newDoc, docErr := recordToDoc(record, vec)
		if docErr != nil {
			s.mu.Unlock()
			continue
		}
		_, upsertErr := s.coll.Upsert([]*zvec.Doc{newDoc})
		newDoc.Destroy()
		if upsertErr != nil {
			s.mu.Unlock()
			continue
		}
		if flushErr := s.coll.Flush(); flushErr != nil {
			s.mu.Unlock()
			continue
		}
		s.mu.Unlock()
		embedded++
	}
	return embedded, nil
}

func newSchema(dim int) (*zvec.CollectionSchema, error) {
	schema := zvec.NewCollectionSchema("agent_memoryd")

	// String fields with FTS index on summary (and _tag for wildcard list) for text search
	for _, fd := range []struct {
		name    string
		withFTS bool
	}{
		{"kind", false},
		{"project", false},
		{"source", false},
		{"summary", true},
		{"_tag", true}, // constant "mem" on every doc; enables SetMatchString("mem") to list all
		{"body", false},
		{"created_at", false},
		{"updated_at", false},
	} {
		field := zvec.NewFieldSchema(fd.name, zvec.DataTypeString, false, 0)
		if fd.withFTS {
			ftsParams, err := zvec.NewFTSIndexParams("standard", nil, "")
			if err != nil {
				field.Destroy()
				schema.Destroy()
				return nil, fmt.Errorf("new fts index params: %w", err)
			}
			if err := field.SetIndexParams(ftsParams); err != nil {
				ftsParams.Destroy()
				field.Destroy()
				schema.Destroy()
				return nil, fmt.Errorf("set fts index params on %s: %w", fd.name, err)
			}
			ftsParams.Destroy()
		}
		if err := schema.AddField(field); err != nil {
			field.Destroy()
			schema.Destroy()
			return nil, fmt.Errorf("add field %s: %w", fd.name, err)
		}
		field.Destroy()
	}

	// Vector field - nullable=true for best-effort embedding (pending vectors stored as null)
	vector := zvec.NewFieldSchema("embedding", zvec.DataTypeVectorFP32, true, uint32(dim))
	hnswParams, err := zvec.NewHNSWIndexParams(zvec.MetricTypeCosine, 16, 200)
	if err != nil {
		vector.Destroy()
		schema.Destroy()
		return nil, fmt.Errorf("new hnsw index params: %w", err)
	}
	if err := vector.SetIndexParams(hnswParams); err != nil {
		hnswParams.Destroy()
		vector.Destroy()
		schema.Destroy()
		return nil, fmt.Errorf("set hnsw params: %w", err)
	}
	hnswParams.Destroy()
	if err := schema.AddField(vector); err != nil {
		vector.Destroy()
		schema.Destroy()
		return nil, fmt.Errorf("add embedding field: %w", err)
	}
	vector.Destroy()

	// embedding_dim tracks whether a document's embedding is present (>0) or pending (0).
	// This scalar field is reliable across Fetch calls and avoids dependence on zvec's
	// nullable-vector introspection, which does not survive Fetch without IncludeVector.
	dimField := zvec.NewFieldSchema("embedding_dim", zvec.DataTypeInt32, true, 0)
	if err := schema.AddField(dimField); err != nil {
		dimField.Destroy()
		schema.Destroy()
		return nil, fmt.Errorf("add embedding_dim field: %w", err)
	}
	dimField.Destroy()

	return schema, nil
}

func recordToDoc(record Record, vec []float32) (*zvec.Doc, error) {
	doc := zvec.NewDoc()
	doc.SetPK(sanitizePK(record.ID))
	fields := map[string]string{
		"kind":       record.Kind,
		"project":    record.Project,
		"source":     record.Source,
		"summary":    record.Summary,
		"_tag":       "mem", // constant marker; allows SetMatchString("mem") to enumerate all docs
		"body":       record.Body,
		"created_at": record.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"updated_at": record.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	for name, value := range fields {
		if err := doc.AddStringField(name, value); err != nil {
			doc.Destroy()
			return nil, fmt.Errorf("add field %s: %w", name, err)
		}
	}
	if vec != nil {
		if err := doc.AddVectorFP32Field("embedding", vec); err != nil {
			doc.Destroy()
			return nil, fmt.Errorf("add embedding: %w", err)
		}
		// Record that the embedding is present (dim > 0 = embedded, 0 = pending).
		if err := doc.AddInt32Field("embedding_dim", int32(len(vec))); err != nil {
			doc.Destroy()
			return nil, fmt.Errorf("add embedding_dim: %w", err)
		}
	} else {
		// Set null to indicate pending embedding; embedding_dim=0 marks pending state.
		if err := doc.SetFieldNull("embedding"); err != nil {
			doc.Destroy()
			return nil, fmt.Errorf("set embedding null: %w", err)
		}
		if err := doc.AddInt32Field("embedding_dim", 0); err != nil {
			doc.Destroy()
			return nil, fmt.Errorf("add embedding_dim pending: %w", err)
		}
	}
	return doc, nil
}

func docToRecord(doc *zvec.Doc) (Record, error) {
	kind, _ := doc.GetStringField("kind")
	project, _ := doc.GetStringField("project")
	source, _ := doc.GetStringField("source")
	summary, _ := doc.GetStringField("summary")
	body, _ := doc.GetStringField("body")
	createdAt, _ := doc.GetStringField("created_at")
	updatedAt, _ := doc.GetStringField("updated_at")
	created, _ := time.Parse("2006-01-02T15:04:05Z07:00", createdAt)
	updated, _ := time.Parse("2006-01-02T15:04:05Z07:00", updatedAt)
	return Record{
		ID:        doc.GetPK(),
		Kind:      kind,
		Project:   project,
		Source:    source,
		Summary:   summary,
		Body:      body,
		CreatedAt: created,
		UpdatedAt: updated,
	}, nil
}

func filterExpr(req SearchRequest) string {
	var filters []string
	if req.Project != "" {
		filters = append(filters, "project = '"+escape(req.Project)+"'")
	}
	if req.Kind != "" {
		filters = append(filters, "kind = '"+escape(req.Kind)+"'")
	}
	return strings.Join(filters, " and ")
}

func escape(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

// pkInvalidChars matches any character NOT in the zvec-accepted allowlist.
// zvec rejects ":", "/", space, and any non-ASCII byte in document primary keys;
// only [A-Za-z0-9_#-] are accepted. Every other character is mapped to "_".
var pkInvalidChars = regexp.MustCompile(`[^A-Za-z0-9_#\-]`)

// pkMaxLen is the maximum primary-key length accepted by the zvec native lib.
const pkMaxLen = 64

// sanitizePK maps every character outside [A-Za-z0-9_#-] to "_" so that
// namespaced IDs like "note:abc", "session/x y", etc. can be stored in zvec.
// If the sanitized string still exceeds pkMaxLen (64), it is truncated to a
// 32-char prefix + "#" + first 31 hex digits of sha256(full sanitized string),
// producing a stable, collision-resistant 64-char key.
// Applied symmetrically on all write and lookup paths so callers can always
// round-trip using the original colon-separated ID in Get/Forget calls.
func sanitizePK(id string) string {
	s := pkInvalidChars.ReplaceAllString(id, "_")
	if len(s) <= pkMaxLen {
		return s
	}
	// Truncate: keep first 32 chars, separator "#", then 31 hex chars of hash.
	sum := sha256.Sum256([]byte(s))
	h := hex.EncodeToString(sum[:])[:31]
	return s[:32] + "#" + h
}

func (s *Store) docsToLiveResultsLocked(docs []*zvec.Doc) ([]SearchResult, error) {
	liveDocs, err := s.fetchLiveDocsLocked(docs)
	if err != nil {
		return nil, err
	}
	defer zvec.FreeDocs(liveDocs)

	liveByPK := make(map[string]*zvec.Doc, len(liveDocs))
	for _, doc := range liveDocs {
		liveByPK[doc.GetPK()] = doc
	}
	results := make([]SearchResult, 0, len(docs))
	for _, doc := range docs {
		liveDoc, ok := liveByPK[doc.GetPK()]
		if !ok {
			continue
		}
		kind, _ := liveDoc.GetStringField("kind")
		project, _ := liveDoc.GetStringField("project")
		source, _ := liveDoc.GetStringField("source")
		summary, _ := liveDoc.GetStringField("summary")
		results = append(results, SearchResult{
			ID:      doc.GetPK(),
			Kind:    kind,
			Project: project,
			Source:  source,
			Summary: summary,
			Score:   float64(doc.GetScore()),
		})
	}
	return results, nil
}

// fetchLiveDocsLocked re-fetches query hits by PK so stale zvec index entries
// left behind after Delete cannot surface through Search/List after Get returns
// ErrNotFound. Caller must hold s.mu.
func (s *Store) fetchLiveDocsLocked(docs []*zvec.Doc) ([]*zvec.Doc, error) {
	pks := make([]string, 0, len(docs))
	seen := make(map[string]struct{}, len(docs))
	for _, doc := range docs {
		pk := doc.GetPK()
		if pk == "" {
			continue
		}
		if _, ok := seen[pk]; ok {
			continue
		}
		seen[pk] = struct{}{}
		pks = append(pks, pk)
	}
	if len(pks) == 0 {
		return nil, nil
	}
	liveDocs, err := s.coll.Fetch(pks, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch live docs: %w", err)
	}
	return liveDocs, nil
}
