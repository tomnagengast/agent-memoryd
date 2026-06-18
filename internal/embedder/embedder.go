package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

var ErrDisabled = errors.New("embedder not configured")

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
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
