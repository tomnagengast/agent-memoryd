package embedder_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/embedder"
)

func TestCommandEmbedsViaBash(t *testing.T) {
	t.Parallel()
	cmd := embedder.Command{
		Argv:    []string{"bash", "-c", `echo '[0.1, 0.2, 0.3]'`},
		Timeout: 5 * time.Second,
	}
	vec, err := cmd.Embed(context.Background(), "test input")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(vec))
	}
}

func TestCommandReturnsErrDisabledOnEmptyArgv(t *testing.T) {
	t.Parallel()
	cmd := embedder.Command{}
	_, err := cmd.Embed(context.Background(), "test")
	if !errors.Is(err, embedder.ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestCommandTimesOut(t *testing.T) {
	t.Parallel()
	cmd := embedder.Command{
		Argv:    []string{"sleep", "10"},
		Timeout: 10 * time.Millisecond,
	}
	_, err := cmd.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestDisabledReturnsErrDisabled(t *testing.T) {
	t.Parallel()
	var d embedder.Disabled
	_, err := d.Embed(context.Background(), "test")
	if !errors.Is(err, embedder.ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}
