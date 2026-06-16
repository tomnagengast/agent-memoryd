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
			record, err := parseTranscript(path, info)
			if err != nil || record.SourceMaterial == "" {
				return nil
			}
			created, err := s.addTranscriptMemories(ctx, store, record, info)
			if err != nil {
				return err
			}
			ingested += created
			return nil
		})
		if err != nil {
			return ingested, err
		}
	}
	return ingested, nil
}

type transcriptRecord struct {
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

func parseTranscript(path string, info fs.FileInfo) (transcriptRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return transcriptRecord{}, err
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
		return transcriptRecord{}, err
	}
	if firstUser == "" && lastUser == "" && assistantTurns == 0 && toolCalls == 0 {
		return transcriptRecord{}, nil
	}
	project := filepath.Base(cwd)
	if project == "." || project == "/" || project == "" {
		project = filepath.Base(filepath.Dir(path))
	}
	return transcriptRecord{
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

func (s Scanner) addTranscriptMemories(ctx context.Context, store *memory.Store, record transcriptRecord, info fs.FileInfo) (int, error) {
	if s.Summarizer == nil {
		return 0, fmt.Errorf("transcript summarizer is not configured")
	}
	existing, err := summarizer.ExistingMemoryRefs(ctx, store, record.Project, s.MemoryContextLimit)
	if err != nil {
		return 0, err
	}
	result, err := s.Summarizer.Summarize(ctx, summarizer.Request{
		Producer:         "transcript",
		Project:          record.Project,
		Source:           record.Path,
		DetailReference:  "Transcript: " + record.Path,
		SourceMaterial:   record.summarizerInput(),
		ExistingMemories: existing,
	})
	if err != nil {
		return 0, err
	}
	for i, item := range result.Memories {
		kind := item.Kind
		if kind == "" {
			kind = "session"
		}
		body := summarizer.EnsureDetailReference(item.Body, "Transcript: "+record.Path)
		if _, err := store.Add(ctx, memory.AddRequest{
			ID:      fmt.Sprintf("%s:%02d", record.ID, i),
			Kind:    kind,
			Project: record.Project,
			Source:  record.Path,
			Summary: item.Summary,
			Body:    body,
			Now:     info.ModTime().UTC(),
		}); err != nil {
			return i, err
		}
	}
	return len(result.Memories), nil
}

func (r transcriptRecord) summarizerInput() string {
	return fmt.Sprintf("Transcript: %s\nCWD: %s\nModified: %s\nAssistant turns: %d\nTool calls: %d\nFirst user prompt: %s\nLast user prompt: %s\n\nRaw transcript JSONL:\n%s",
		r.Path,
		r.CWD,
		r.Modified.Format(time.RFC3339),
		r.AssistantTurns,
		r.ToolCalls,
		r.FirstUser,
		r.LastUser,
		r.SourceMaterial,
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
