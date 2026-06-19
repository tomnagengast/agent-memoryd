package memory

import "testing"

func TestNormalizeEmpty(t *testing.T) {
	t.Parallel()
	out := normalize(nil)
	if len(out) != 0 {
		t.Fatal("expected empty")
	}
}

func TestNormalizeSingleValue(t *testing.T) {
	t.Parallel()
	out := normalize([]float64{5.0})
	if out[0] != 1.0 {
		t.Fatalf("expected 1.0, got %v", out[0])
	}
}

func TestNormalizeRange(t *testing.T) {
	t.Parallel()
	out := normalize([]float64{0, 50, 100})
	if out[0] != 0.0 || out[1] != 0.5 || out[2] != 1.0 {
		t.Fatalf("unexpected normalization: %v", out)
	}
}

func TestBlendBothLegs(t *testing.T) {
	t.Parallel()
	fts := []SearchResult{{ID: "a", Score: 1.0}, {ID: "b", Score: 0.5}}
	vec := []SearchResult{{ID: "a", Score: 0.8}, {ID: "c", Score: 1.0}}
	w := BlendWeights{FTS: 0.5, Vector: 0.5}
	results := blend(fts, vec, w, 10)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].ID != "a" {
		t.Fatalf("expected 'a' first, got %s", results[0].ID)
	}
}

func TestBlendTreatsLowerVectorDistanceAsBetter(t *testing.T) {
	t.Parallel()
	vec := []SearchResult{{ID: "near", Score: 0.1}, {ID: "far", Score: 0.9}}
	w := BlendWeights{FTS: 0.5, Vector: 0.5}
	results := blend(nil, vec, w, 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "near" {
		t.Fatalf("expected lower vector distance first, got %s", results[0].ID)
	}
}

func TestBlendFTSOnly(t *testing.T) {
	t.Parallel()
	fts := []SearchResult{{ID: "a", Score: 1.0}}
	w := BlendWeights{FTS: 1.0, Vector: 0.0}
	results := blend(fts, nil, w, 10)
	if len(results) != 1 || results[0].ID != "a" {
		t.Fatal("expected single FTS result")
	}
}

func TestBlendCapsVectorOnlyResults(t *testing.T) {
	t.Parallel()
	vec := []SearchResult{
		{ID: "near", Score: 0.1},
		{ID: "middle", Score: 0.2},
		{ID: "far", Score: 0.9},
	}
	w := BlendWeights{FTS: 0.0, Vector: 1.0}
	uncapped := blend(nil, vec, w, 0)
	if len(uncapped) != 3 {
		t.Fatalf("expected uncapped vector-only union of 3, got %d", len(uncapped))
	}
	results := blend(nil, vec, w, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "near" || results[1].ID != "middle" {
		t.Fatalf("unexpected capped vector order: %+v", results)
	}
}

func TestBlendCapsHybridUnionResults(t *testing.T) {
	t.Parallel()
	fts := []SearchResult{
		{ID: "fts-a", Score: 10},
		{ID: "fts-b", Score: 9},
		{ID: "shared", Score: 8},
	}
	vec := []SearchResult{
		{ID: "shared", Score: 0.1},
		{ID: "vec-a", Score: 0.2},
		{ID: "vec-b", Score: 0.3},
	}
	w := BlendWeights{FTS: 0.5, Vector: 0.5}
	uncapped := blend(fts, vec, w, 0)
	if len(uncapped) != 5 {
		t.Fatalf("expected uncapped hybrid union of 5, got %d", len(uncapped))
	}
	results := blend(fts, vec, w, 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d: %+v", len(results), results)
	}
	for _, result := range results {
		if result.ID == "" {
			t.Fatalf("unexpected empty result ID: %+v", results)
		}
	}
}
