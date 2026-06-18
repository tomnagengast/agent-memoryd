package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/embedder"
	"github.com/tomnagengast/agent-memoryd/internal/flock"
	zvec "github.com/zvec-ai/zvec-go"
)

var (
	ErrEmptyBody  = errors.New("memory body is empty")
	ErrNotFound   = errors.New("memory not found")
	ErrDimension  = errors.New("embedding dimension mismatch")
)

type Store struct {
	coll     *zvec.Collection
	path     string
	lock     flock.Locker
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

type OpenConfig struct {
	ZvecPath     string
	EmbeddingDim int
	LockTimeout  time.Duration
	FTSWeight    float64
	VectorWeight float64
	Embedder     embedder.Embedder
}

func Open(cfg OpenConfig) (*Store, error) {
	if err := zvec.Initialize(nil); err != nil {
		return nil, fmt.Errorf("initialize zvec: %w", err)
	}
	lockPath := cfg.ZvecPath + ".lock"
	locker := &flock.FileLocker{Path: lockPath, LockTimeout: cfg.LockTimeout}

	var coll *zvec.Collection
	var err error
	if _, statErr := os.Stat(cfg.ZvecPath); statErr == nil {
		coll, err = zvec.Open(cfg.ZvecPath, nil)
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
	emb := cfg.Embedder
	if emb == nil {
		emb = embedder.Disabled{}
	}
	return &Store{
		coll:     coll,
		path:     cfg.ZvecPath,
		lock:     locker,
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

func (s *Store) Add(ctx context.Context, req AddRequest) (Record, error) {
	record, err := NewRecord(req)
	if err != nil {
		return Record{}, err
	}
	vec, embedErr := s.embedder.Embed(ctx, record.Summary+"\n"+record.Body)
	if embedErr != nil && !errors.Is(embedErr, embedder.ErrDisabled) {
		vec = nil // treat other errors as pending - vector will be backfilled
	}
	if vec != nil && len(vec) != s.dim {
		return Record{}, fmt.Errorf("%w: expected %d, got %d", ErrDimension, s.dim, len(vec))
	}
	err = s.lock.WithLock(ctx, func() error {
		doc, docErr := recordToDoc(record, vec)
		if docErr != nil {
			return docErr
		}
		defer doc.Destroy()
		if _, upsertErr := s.coll.Upsert([]*zvec.Doc{doc}); upsertErr != nil {
			return fmt.Errorf("upsert: %w", upsertErr)
		}
		return s.coll.Flush()
	})
	if err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Store) Get(ctx context.Context, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	docs, err := s.coll.Fetch([]string{id}, nil)
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
	return s.lock.WithLock(ctx, func() error {
		result, err := s.coll.Delete([]string{id})
		if err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		if result.SuccessCount == 0 {
			return ErrNotFound
		}
		return s.coll.Flush()
	})
}

func (s *Store) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}

	filter := filterExpr(req)

	// FTS leg using zvec FTS API: NewFTS() + SetMatchString + query.SetFTS
	ftsQuery := zvec.NewSearchQuery()
	defer ftsQuery.Destroy()
	if err := ftsQuery.SetFieldName("summary"); err != nil {
		return nil, fmt.Errorf("set fts field name: %w", err)
	}
	fts := zvec.NewFTS()
	if err := fts.SetMatchString(req.Query); err != nil {
		fts.Destroy()
		return nil, fmt.Errorf("set fts match string: %w", err)
	}
	if err := ftsQuery.SetFTS(fts); err != nil {
		fts.Destroy()
		return nil, fmt.Errorf("set fts query: %w", err)
	}
	fts.Destroy()
	if err := ftsQuery.SetTopK(limit); err != nil {
		return nil, err
	}
	if filter != "" {
		if err := ftsQuery.SetFilter(filter); err != nil {
			return nil, err
		}
	}
	if err := ftsQuery.SetOutputFields([]string{"kind", "project", "source", "summary"}); err != nil {
		return nil, err
	}
	ftsDocs, err := s.coll.Query(ftsQuery)
	if err != nil {
		return nil, fmt.Errorf("fts query: %w", err)
	}
	defer zvec.FreeDocs(ftsDocs)
	ftsResults := docsToResults(ftsDocs)

	// Vector leg - skip if embedder disabled or fails
	var vecResults []SearchResult
	queryVec, embedErr := s.embedder.Embed(ctx, req.Query)
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
						vecResults = docsToResults(vecDocs)
					}
				}
			}
		}
	}

	// Blend results
	if len(vecResults) == 0 {
		return ftsResults, nil
	}
	return blend(ftsResults, vecResults, s.weights), nil
}

func (s *Store) List(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stats, err := s.coll.GetStats()
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}
	if stats.DocCount == 0 {
		return nil, nil
	}
	// Use FTS wildcard to enumerate all documents
	query := zvec.NewSearchQuery()
	defer query.Destroy()
	if err := query.SetFieldName("summary"); err != nil {
		return nil, fmt.Errorf("list set field name: %w", err)
	}
	fts := zvec.NewFTS()
	// Wildcard * matches all documents; SetQueryString uses BM25/Lucene syntax
	if err := fts.SetQueryString("*"); err != nil {
		fts.Destroy()
		return nil, fmt.Errorf("list set query string: %w", err)
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
	records := make([]Record, 0, len(docs))
	for _, doc := range docs {
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
	stats, err := s.coll.GetStats()
	if err != nil {
		return Status{}, fmt.Errorf("get stats: %w", err)
	}
	return Status{
		Path:        s.path,
		Backend:     "zvec",
		MemoryCount: int(stats.DocCount),
	}, nil
}

func (s *Store) Backfill(ctx context.Context) (int, error) {
	// Embeds rows with null vectors - placeholder; full implementation deferred to Phase 4
	return 0, nil
}

func newSchema(dim int) (*zvec.CollectionSchema, error) {
	schema := zvec.NewCollectionSchema("agent_memoryd")

	// String fields with FTS index on summary for text search
	for _, fd := range []struct {
		name    string
		withFTS bool
	}{
		{"kind", false},
		{"project", false},
		{"source", false},
		{"summary", true},
		{"body", false},
		{"created_at", false},
		{"updated_at", false},
	} {
		field := zvec.NewFieldSchema(fd.name, zvec.DataTypeString, false, 0)
		if fd.withFTS {
			ftsParams, err := zvec.NewFTSIndexParams("default", nil, "")
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

	return schema, nil
}

func recordToDoc(record Record, vec []float32) (*zvec.Doc, error) {
	doc := zvec.NewDoc()
	doc.SetPK(record.ID)
	fields := map[string]string{
		"kind":       record.Kind,
		"project":    record.Project,
		"source":     record.Source,
		"summary":    record.Summary,
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
	} else {
		// Set null to indicate pending embedding
		if err := doc.SetFieldNull("embedding"); err != nil {
			doc.Destroy()
			return nil, fmt.Errorf("set embedding null: %w", err)
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

func docsToResults(docs []*zvec.Doc) []SearchResult {
	results := make([]SearchResult, 0, len(docs))
	for _, doc := range docs {
		kind, _ := doc.GetStringField("kind")
		project, _ := doc.GetStringField("project")
		source, _ := doc.GetStringField("source")
		summary, _ := doc.GetStringField("summary")
		results = append(results, SearchResult{
			ID:      doc.GetPK(),
			Kind:    kind,
			Project: project,
			Source:  source,
			Summary: summary,
			Score:   float64(doc.GetScore()),
		})
	}
	return results
}
