package spool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

type GitEvent struct {
	Repo      string    `json:"repo"`
	SHA       string    `json:"sha"`
	CreatedAt time.Time `json:"created_at"`
}

func EnqueueGit(dir string, event GitEvent) (string, error) {
	if strings.TrimSpace(event.Repo) == "" {
		return "", fmt.Errorf("repo is required")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create spool dir: %w", err)
	}
	name, err := randomName("git", ".json")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, append(data, '\n'), 0o644)
}

func ProcessGit(ctx context.Context, dir string, store *memory.Store) (int, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read spool dir: %w", err)
	}
	var processed int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "git-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return processed, fmt.Errorf("read git event: %w", err)
		}
		var event GitEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return processed, fmt.Errorf("decode git event %s: %w", path, err)
		}
		if err := addGitMemory(ctx, store, event); err != nil {
			return processed, err
		}
		if err := os.Remove(path); err != nil {
			return processed, fmt.Errorf("remove git event: %w", err)
		}
		processed++
	}
	return processed, nil
}

func addGitMemory(ctx context.Context, store *memory.Store, event GitEvent) error {
	summary, body := summarizeCommit(ctx, event)
	id := "git:" + stableID(event.Repo+"@"+event.SHA)
	_, err := store.Add(ctx, memory.AddRequest{
		ID:      id,
		Kind:    "git-summary",
		Project: filepath.Base(event.Repo),
		Source:  event.Repo + "@" + event.SHA,
		Summary: summary,
		Body:    body,
		Now:     event.CreatedAt,
	})
	return err
}

func summarizeCommit(ctx context.Context, event GitEvent) (string, string) {
	sha := strings.TrimSpace(event.SHA)
	if sha == "" {
		sha = "HEAD"
	}
	cmd := exec.CommandContext(ctx, "git", "-C", event.Repo, "show", "--stat", "--format=%h %s%n%n%b", "--no-ext-diff", sha)
	out, err := cmd.Output()
	if err != nil {
		return "Git update in " + filepath.Base(event.Repo), fmt.Sprintf("Repo: %s\nCommit: %s\n", event.Repo, sha)
	}
	text := strings.TrimSpace(string(out))
	first := strings.Split(text, "\n")[0]
	if first == "" {
		first = "Git update in " + filepath.Base(event.Repo)
	}
	return first, text
}

func randomName(prefix, suffix string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(buf[:]) + suffix, nil
}

func stableID(value string) string {
	var out uint64 = 1469598103934665603
	for _, b := range []byte(value) {
		out ^= uint64(b)
		out *= 1099511628211
	}
	return fmt.Sprintf("%016x", out)
}
