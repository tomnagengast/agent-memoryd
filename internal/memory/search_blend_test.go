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
	results := blend(fts, vec, w)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].ID != "a" {
		t.Fatalf("expected 'a' first, got %s", results[0].ID)
	}
}

func TestBlendFTSOnly(t *testing.T) {
	t.Parallel()
	fts := []SearchResult{{ID: "a", Score: 1.0}}
	w := BlendWeights{FTS: 1.0, Vector: 0.0}
	results := blend(fts, nil, w)
	if len(results) != 1 || results[0].ID != "a" {
		t.Fatal("expected single FTS result")
	}
}
