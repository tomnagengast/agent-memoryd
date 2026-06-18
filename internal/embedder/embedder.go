package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

var ErrDisabled = errors.New("embedder not configured")

const (
	ProviderDisabled = "disabled"
	ProviderCommand  = "command"
	ProviderOllama   = "ollama"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type ProviderConfig struct {
	Provider string
	Command  []string
	Timeout  time.Duration
	Model    string
	URL      string
}

func EffectiveProvider(cfg ProviderConfig) string {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if len(cfg.Command) > 0 && (provider == "" || provider == ProviderDisabled || provider == ProviderCommand) {
		return ProviderCommand
	}
	switch provider {
	case ProviderOllama:
		return ProviderOllama
	case ProviderCommand:
		return ProviderCommand
	default:
		return ProviderDisabled
	}
}

func NewProvider(cfg ProviderConfig) (Embedder, error) {
	switch EffectiveProvider(cfg) {
	case ProviderCommand:
		return Command{Argv: cfg.Command, Timeout: cfg.Timeout}, nil
	case ProviderOllama:
		return Ollama{URL: cfg.URL, Model: cfg.Model, Timeout: cfg.Timeout}, nil
	default:
		return Disabled{}, nil
	}
}

type Command struct {
	Argv    []string
	Timeout time.Duration
}

func (c Command) Embed(ctx context.Context, text string) ([]float32, error) {
	if len(c.Argv) == 0 || c.Argv[0] == "" {
		return nil, ErrDisabled
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.Stdin = bytes.NewReader([]byte(text))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("run embedder: %w: subprocess output redacted (stdout_bytes=%d stderr_bytes=%d)", err, stdout.Len(), stderr.Len())
	}
	var vec []float32
	if err := json.Unmarshal(stdout.Bytes(), &vec); err != nil {
		return nil, fmt.Errorf("decode embedder output: %w", err)
	}
	return vec, nil
}

type Disabled struct{}

func (Disabled) Embed(context.Context, string) ([]float32, error) {
	return nil, ErrDisabled
}

type Ollama struct {
	URL     string
	Model   string
	Timeout time.Duration
	Client  *http.Client
}

func (o Ollama) Embed(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(o.URL) == "" || strings.TrimSpace(o.Model) == "" {
		return nil, ErrDisabled
	}
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}
	body, err := json.Marshal(map[string]any{
		"model": o.Model,
		"input": text,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ollamaEmbedURL(o.URL), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create ollama embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call ollama embed API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama embed API returned %s: response body redacted", resp.Status)
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode ollama embed response: %w", err)
	}
	if len(out.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama embed response contained no embeddings")
	}
	return out.Embeddings[0], nil
}

func ollamaEmbedURL(base string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/") + "/api/embed"
}
