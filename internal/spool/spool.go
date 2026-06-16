package spool

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/ingeststate"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/summarizer"
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

func ProcessGit(ctx context.Context, dir string, store *memory.Store, agent summarizer.Agent, contextLimit int, state *ingeststate.State) (int, error) {
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
		key := "git:" + entry.Name()
		fingerprint := eventFingerprint(data)
		now := time.Now()
		if !state.ShouldProcess(key, fingerprint, now) {
			continue
		}
		if err := addGitMemory(ctx, store, event, agent, contextLimit); err != nil {
			if state != nil {
				input := state.MarkFailed(key, fingerprint, err, now)
				if input.Status == "quarantined" {
					if moveErr := moveFailedEvent(dir, path); moveErr != nil {
						return processed, moveErr
					}
				}
				continue
			}
			return processed, err
		}
		if err := os.Remove(path); err != nil {
			return processed, fmt.Errorf("remove git event: %w", err)
		}
		state.MarkProcessed(key, fingerprint, now)
		processed++
	}
	return processed, nil
}

func moveFailedEvent(dir, path string) error {
	failedDir := filepath.Join(dir, "failed")
	if err := os.MkdirAll(failedDir, 0o755); err != nil {
		return fmt.Errorf("create failed spool dir: %w", err)
	}
	dst := filepath.Join(failedDir, filepath.Base(path))
	if err := os.Rename(path, dst); err != nil {
		return fmt.Errorf("move failed git event: %w", err)
	}
	return nil
}

func addGitMemory(ctx context.Context, store *memory.Store, event GitEvent, agent summarizer.Agent, contextLimit int) error {
	if agent == nil {
		return fmt.Errorf("git summarizer is not configured")
	}
	project := filepath.Base(event.Repo)
	sha := strings.TrimSpace(event.SHA)
	if sha == "" {
		sha = "HEAD"
	}
	commitText := summarizeCommit(ctx, event)
	existing, err := summarizer.ExistingMemoryRefs(ctx, store, project, contextLimit)
	if err != nil {
		return err
	}
	source := event.Repo + "@" + sha
	detailReference := "Commit: " + sha + "\nRepo: " + event.Repo
	result, err := agent.Summarize(ctx, summarizer.Request{
		Producer:         "git",
		Project:          project,
		Source:           source,
		DetailReference:  detailReference,
		SourceMaterial:   commitSummarizerInput(event, sha, commitText),
		ExistingMemories: existing,
	})
	if err != nil {
		return err
	}
	id := "git:" + stableID(source)
	for i, item := range result.Memories {
		kind := item.Kind
		if kind == "" {
			kind = "git-summary"
		}
		body := summarizer.EnsureDetailReference(item.Body, detailReference)
		if _, err := store.Add(ctx, memory.AddRequest{
			ID:      fmt.Sprintf("%s:%02d", id, i),
			Kind:    kind,
			Project: project,
			Source:  source,
			Summary: item.Summary,
			Body:    body,
			Now:     event.CreatedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func summarizeCommit(ctx context.Context, event GitEvent) string {
	sha := strings.TrimSpace(event.SHA)
	if sha == "" {
		sha = "HEAD"
	}
	cmd := exec.CommandContext(ctx, "git", "-C", event.Repo, "show", "--stat", "--format=%h %s%n%n%b", "--no-ext-diff", sha)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("Repo: %s\nCommit: %s\n", event.Repo, sha)
	}
	return strings.TrimSpace(string(out))
}

func commitSummarizerInput(event GitEvent, sha string, commitText string) string {
	return fmt.Sprintf("Repo: %s\nCommit: %s\nCreated at: %s\n\nGit summary:\n%s\n",
		event.Repo,
		sha,
		event.CreatedAt.UTC().Format(time.RFC3339),
		commitText,
	)
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

func eventFingerprint(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}
