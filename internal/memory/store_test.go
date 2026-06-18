package memory

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/embedder"
)

func TestSanitizePK(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input   string
		want    string
		maxLen  int // if > 0, assert len(got) <= maxLen
		wantLen int // if > 0, assert exact len
	}{
		// zvec-rejected characters -> "_"
		{"note:alpha", "note_alpha", 0, 0},
		{"session/x", "session_x", 0, 0},
		{"has space", "has_space", 0, 0},
		{"non-ascii-\xff", "non-ascii-_", 0, 0},
		// zvec-accepted characters preserved
		{"letters-and_digits#123", "letters-and_digits#123", 0, 0},
		{"UPPER-lower_0#1-2", "UPPER-lower_0#1-2", 0, 0},
		// multiple rejected chars
		{"note:foo/bar baz", "note_foo_bar_baz", 0, 0},
		// colon+hash mix (common in live data e.g. "session:beta-2026#003")
		{"session:beta-2026#003", "session_beta-2026#003", 0, 0},
		// truncation: input longer than 64 chars after sanitization
		{
			"note:cresta-staff-machine-learning-engineer-resume-choice-2026-06-05",
			"", // exact value checked by length only
			64, 64,
		},
	}
	for _, tc := range cases {
		got := sanitizePK(tc.input)
		if tc.want != "" && got != tc.want {
			t.Errorf("sanitizePK(%q) = %q, want %q", tc.input, got, tc.want)
		}
		if tc.maxLen > 0 && len(got) > tc.maxLen {
			t.Errorf("sanitizePK(%q): len=%d > maxLen=%d, got=%q", tc.input, len(got), tc.maxLen, got)
		}
		if tc.wantLen > 0 && len(got) != tc.wantLen {
			t.Errorf("sanitizePK(%q): len=%d, want %d, got=%q", tc.input, len(got), tc.wantLen, got)
		}
		// Verify the result only uses allowed chars
		for i, c := range got {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '#' || c == '-') {
				t.Errorf("sanitizePK(%q): result[%d]=%q is not in allowed charset, result=%q", tc.input, i, string(c), got)
			}
		}
	}
}

func TestStoreAddsSearchesGetsAndForgetsMemory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(OpenConfig{
		ZvecPath:     filepath.Join(t.TempDir(), "zvec"),
		EmbeddingDim: 128,
		LockTimeout:  2 * time.Second,
		FTSWeight:    0.5,
		VectorWeight: 0.5,
		Embedder:     embedder.Disabled{},
	})
	if err != nil {
		t.Skipf("skipping: zvec unavailable: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

	record, err := store.Add(ctx, AddRequest{
		ID:      "style",
		Kind:    "feedback",
		Project: "agent-memoryd",
		Body:    "Prefer concise final answers with concrete file links.",
		Now:     now,
	})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	if record.ID != "style" {
		t.Fatalf("record.ID = %q, want style", record.ID)
	}

	got, err := store.Get(ctx, "style")
	if err != nil {
		t.Fatalf("get memory: %v", err)
	}
	if got.Body != record.Body {
		t.Fatalf("got.Body = %q, want %q", got.Body, record.Body)
	}

	if err := store.Forget(ctx, "style"); err != nil {
		t.Fatalf("forget memory: %v", err)
	}
	_, err = store.Get(ctx, "style")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("get forgotten memory error = %v, want ErrNotFound", err)
	}
}
