package embedder_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestEffectiveProviderMapsLegacyCommandConfig(t *testing.T) {
	t.Parallel()
	got := embedder.EffectiveProvider(embedder.ProviderConfig{
		Provider: embedder.ProviderDisabled,
		Command:  []string{"embed"},
	})
	if got != embedder.ProviderCommand {
		t.Fatalf("effective provider = %q, want command", got)
	}
}

func TestNewProviderReturnsOllama(t *testing.T) {
	t.Parallel()
	got, err := embedder.NewProvider(embedder.ProviderConfig{
		Provider: embedder.ProviderOllama,
		URL:      "http://127.0.0.1:11434",
		Model:    "nomic-embed-text",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, ok := got.(embedder.Ollama); !ok {
		t.Fatalf("provider type = %T, want Ollama", got)
	}
}

func TestOllamaEmbedsViaAPI(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Fatalf("path = %q, want /api/embed", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		fmt.Fprint(w, `{"model":"nomic-embed-text","embeddings":[[0.1,0.2,0.3]]}`)
	}))
	defer srv.Close()

	ollama := embedder.Ollama{
		URL:     srv.URL,
		Model:   "nomic-embed-text",
		Timeout: time.Second,
		Client:  srv.Client(),
	}
	vec, err := ollama.Embed(context.Background(), "probe")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("len(vec) = %d, want 3", len(vec))
	}
}

func TestOllamaReturnsRedactedStatusError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "sensitive details", http.StatusNotFound)
	}))
	defer srv.Close()

	ollama := embedder.Ollama{
		URL:    srv.URL,
		Model:  "missing",
		Client: srv.Client(),
	}
	_, err := ollama.Embed(context.Background(), "probe")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, embedder.ErrDisabled) || fmt.Sprint(err) == "" {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(fmt.Sprint(err), "sensitive details") {
		t.Fatalf("error leaked response body: %v", err)
	}
}
