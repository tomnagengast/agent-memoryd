//go:build zvec

package zvecindex

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
	zvec "github.com/zvec-ai/zvec-go"
)

const dim = 128

type Index struct {
	collection *zvec.Collection
	path       string
}

func New(path string) (*Index, error) {
	if err := zvec.Initialize(nil); err != nil {
		return nil, fmt.Errorf("initialize zvec: %w", err)
	}
	var collection *zvec.Collection
	var err error
	if _, statErr := os.Stat(path); statErr == nil {
		collection, err = zvec.Open(path, nil)
	} else {
		schema, schemaErr := schema()
		if schemaErr != nil {
			return nil, schemaErr
		}
		defer schema.Destroy()
		collection, err = zvec.CreateAndOpen(path, schema, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("open zvec collection: %w", err)
	}
	return &Index{collection: collection, path: path}, nil
}

func (i *Index) Name() string {
	return "zvec"
}

func (i *Index) Search(ctx context.Context, _ []memory.Record, req memory.SearchRequest) ([]memory.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := zvec.NewSearchQuery()
	defer query.Destroy()
	if err := query.SetFieldName("embedding"); err != nil {
		return nil, err
	}
	if err := query.SetQueryVector(embed(req.Query)); err != nil {
		return nil, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}
	if err := query.SetTopK(limit); err != nil {
		return nil, err
	}
	if filter := filterExpr(req); filter != "" {
		if err := query.SetFilter(filter); err != nil {
			return nil, err
		}
	}
	if err := query.SetOutputFields([]string{"kind", "project", "source", "summary"}); err != nil {
		return nil, err
	}
	docs, err := i.collection.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query zvec: %w", err)
	}
	defer zvec.FreeDocs(docs)
	results := make([]memory.SearchResult, 0, len(docs))
	for _, doc := range docs {
		kind, _ := doc.GetStringField("kind")
		project, _ := doc.GetStringField("project")
		source, _ := doc.GetStringField("source")
		summary, _ := doc.GetStringField("summary")
		results = append(results, memory.SearchResult{
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

func (i *Index) Upsert(ctx context.Context, record memory.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	doc, err := doc(record)
	if err != nil {
		return err
	}
	defer doc.Destroy()
	if _, err := i.collection.Upsert([]*zvec.Doc{doc}); err != nil {
		return fmt.Errorf("upsert zvec doc: %w", err)
	}
	return i.collection.Flush()
}

func (i *Index) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := i.collection.Delete([]string{id}); err != nil {
		return fmt.Errorf("delete zvec doc: %w", err)
	}
	return i.collection.Flush()
}

func (i *Index) Rebuild(ctx context.Context, records []memory.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	docs := make([]*zvec.Doc, 0, len(records))
	for _, record := range records {
		doc, err := doc(record)
		if err != nil {
			zvec.FreeDocs(docs)
			return err
		}
		docs = append(docs, doc)
	}
	defer zvec.FreeDocs(docs)
	if len(docs) == 0 {
		return nil
	}
	if _, err := i.collection.Upsert(docs); err != nil {
		return fmt.Errorf("rebuild zvec index: %w", err)
	}
	return i.collection.Flush()
}

func schema() (*zvec.CollectionSchema, error) {
	schema := zvec.NewCollectionSchema("agent_memoryd")
	for _, field := range []*zvec.FieldSchema{
		zvec.NewFieldSchema("kind", zvec.DataTypeString, false, 0),
		zvec.NewFieldSchema("project", zvec.DataTypeString, false, 0),
		zvec.NewFieldSchema("source", zvec.DataTypeString, false, 0),
		zvec.NewFieldSchema("summary", zvec.DataTypeString, false, 0),
		zvec.NewFieldSchema("body", zvec.DataTypeString, false, 0),
		zvec.NewFieldSchema("created_at", zvec.DataTypeString, false, 0),
		zvec.NewFieldSchema("updated_at", zvec.DataTypeString, false, 0),
	} {
		if err := schema.AddField(field); err != nil {
			return nil, err
		}
	}
	vector := zvec.NewFieldSchema("embedding", zvec.DataTypeVectorFP32, false, dim)
	params, err := zvec.NewHNSWIndexParams(zvec.MetricTypeCosine, 16, 200)
	if err != nil {
		return nil, err
	}
	if err := vector.SetIndexParams(params); err != nil {
		return nil, err
	}
	if err := schema.AddField(vector); err != nil {
		return nil, err
	}
	return schema, nil
}

func doc(record memory.Record) (*zvec.Doc, error) {
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
			return nil, err
		}
	}
	if err := doc.AddVectorFP32Field("embedding", embed(record.Summary+"\n"+record.Body)); err != nil {
		doc.Destroy()
		return nil, err
	}
	return doc, nil
}

func embed(text string) []float32 {
	vec := make([]float32, dim)
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if token == "" {
			continue
		}
		var h uint32 = 2166136261
		for _, b := range []byte(token) {
			h ^= uint32(b)
			h *= 16777619
		}
		vec[int(h%dim)] += 1
	}
	var sum float32
	for _, v := range vec {
		sum += v * v
	}
	if sum == 0 {
		return vec
	}
	// Avoid importing math for a single rough normalization in the bootstrap embedder.
	norm := sqrt32(sum)
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}

func sqrt32(x float32) float32 {
	z := x
	for range 8 {
		z -= (z*z - x) / (2 * z)
	}
	return z
}

func filterExpr(req memory.SearchRequest) string {
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
