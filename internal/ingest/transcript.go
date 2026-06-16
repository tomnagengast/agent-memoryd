package ingest

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/summarizer"
)

type Scanner struct {
	Roots              []string
	IdleAfter          time.Duration
	Summarizer         summarizer.Agent
	MemoryContextLimit int
}

func (s Scanner) Scan(ctx context.Context, store *memory.Store) (int, error) {
	var ingested int
	for _, root := range s.Roots {
		if err := ctx.Err(); err != nil {
			return ingested, err
		}
		if root == "" {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if s.IdleAfter > 0 && time.Since(info.ModTime()) < s.IdleAfter {
				return nil
			}
			transcript, err := parseTranscript(path, info)
			if err != nil || transcript.SourceMaterial == "" {
				return nil
			}
			records, err := StoreTranscriptMemories(ctx, store, s.Summarizer, s.MemoryContextLimit, transcript, info.ModTime().UTC())
			if err != nil {
				return err
			}
			ingested += len(records)
			return nil
		})
		if err != nil {
			return ingested, err
		}
	}
	return ingested, nil
}

type Transcript struct {
	ID             string
	Project        string
	Path           string
	CWD            string
	Modified       time.Time
	AssistantTurns int
	ToolCalls      int
	FirstUser      string
	LastUser       string
	SourceMaterial string
}

func ReadTranscript(path string) (Transcript, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Transcript{}, err
	}
	return parseTranscript(path, info)
}

func parseTranscript(path string, info fs.FileInfo) (Transcript, error) {
	file, err := os.Open(path)
	if err != nil {
		return Transcript{}, err
	}
	defer file.Close()

	var cwd string
	var firstUser string
	var lastUser string
	var assistantTurns int
	var toolCalls int
	var raw strings.Builder

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 16*1024*1024)
	for scanner.Scan() {
		raw.Write(scanner.Bytes())
		raw.WriteByte('\n')
		var obj map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &obj); err != nil {
			continue
		}
		if cwd == "" {
			if value, ok := obj["cwd"].(string); ok {
				cwd = value
			}
		}
		role, text := messageText(obj)
		switch role {
		case "user":
			if strings.TrimSpace(text) == "" || strings.HasPrefix(strings.TrimSpace(text), "# AGENTS.md instructions") {
				continue
			}
			if firstUser == "" {
				firstUser = strings.TrimSpace(text)
			}
			lastUser = strings.TrimSpace(text)
		case "assistant":
			assistantTurns++
		}
		if isToolCall(obj) {
			toolCalls++
		}
	}
	if err := scanner.Err(); err != nil {
		return Transcript{}, err
	}
	if firstUser == "" && lastUser == "" && assistantTurns == 0 && toolCalls == 0 {
		return Transcript{}, nil
	}
	project := filepath.Base(cwd)
	if project == "." || project == "/" || project == "" {
		project = filepath.Base(filepath.Dir(path))
	}
	return Transcript{
		ID:             "session:" + fileID(path, info),
		Project:        project,
		Path:           path,
		CWD:            cwd,
		Modified:       info.ModTime().UTC(),
		AssistantTurns: assistantTurns,
		ToolCalls:      toolCalls,
		FirstUser:      firstUser,
		LastUser:       lastUser,
		SourceMaterial: raw.String(),
	}, nil
}

func StoreTranscriptMemories(ctx context.Context, store *memory.Store, agent summarizer.Agent, contextLimit int, transcript Transcript, now time.Time) ([]memory.Record, error) {
	if agent == nil {
		return nil, fmt.Errorf("transcript summarizer is not configured")
	}
	existing, err := summarizer.ExistingMemoryRefs(ctx, store, transcript.Project, contextLimit)
	if err != nil {
		return nil, err
	}
	result, err := agent.Summarize(ctx, summarizer.Request{
		Producer:         "transcript",
		Project:          transcript.Project,
		Source:           transcript.Path,
		DetailReference:  transcript.DetailReference(),
		SourceMaterial:   transcript.SummarizerInput(),
		ExistingMemories: existing,
	})
	if err != nil {
		return nil, err
	}
	records := make([]memory.Record, 0, len(result.Memories))
	for i, item := range result.Memories {
		kind := item.Kind
		if kind == "" {
			kind = "session"
		}
		body := summarizer.EnsureDetailReference(item.Body, transcript.DetailReference())
		record, err := store.Add(ctx, memory.AddRequest{
			ID:      fmt.Sprintf("%s:%02d", transcript.ID, i),
			Kind:    kind,
			Project: transcript.Project,
			Source:  transcript.Path,
			Summary: item.Summary,
			Body:    body,
			Now:     now,
		})
		if err != nil {
			return records, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (t Transcript) DetailReference() string {
	return "Transcript: " + t.Path
}

func (t Transcript) SummarizerInput() string {
	return fmt.Sprintf("Transcript: %s\nCWD: %s\nModified: %s\nAssistant turns: %d\nTool calls: %d\nFirst user prompt: %s\nLast user prompt: %s\n\nRaw transcript JSONL:\n%s",
		t.Path,
		t.CWD,
		t.Modified.Format(time.RFC3339),
		t.AssistantTurns,
		t.ToolCalls,
		t.FirstUser,
		t.LastUser,
		t.SourceMaterial,
	)
}

func messageText(obj map[string]any) (string, string) {
	msg, ok := obj["message"].(map[string]any)
	if !ok {
		if payload, ok := obj["payload"].(map[string]any); ok {
			msg = payload
		} else {
			return "", ""
		}
	}
	role, _ := msg["role"].(string)
	switch content := msg["content"].(type) {
	case string:
		return role, content
	case []any:
		var parts []string
		for _, item := range content {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return role, strings.Join(parts, "\n")
	}
	return role, ""
}

func isToolCall(obj map[string]any) bool {
	if itemType, _ := obj["type"].(string); itemType == "tool_use" {
		return true
	}
	if payload, ok := obj["payload"].(map[string]any); ok {
		if typ, _ := payload["type"].(string); typ == "function_call" {
			return true
		}
	}
	return false
}

func fileID(path string, info fs.FileInfo) string {
	h := sha1.New()
	_, _ = h.Write([]byte(path))
	_, _ = h.Write([]byte(info.ModTime().UTC().Format(time.RFC3339Nano)))
	_, _ = h.Write([]byte(fmt.Sprint(info.Size())))
	return hex.EncodeToString(h.Sum(nil))[:16]
}
