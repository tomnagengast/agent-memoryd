package ingest

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/ingeststate"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/summarizer"
)

type Scanner struct {
	Roots              []string
	IdleAfter          time.Duration
	NotBefore          time.Time
	Summarizer         summarizer.Agent
	MemoryContextLimit int
	State              *ingeststate.State
	Now                time.Time
}

func (s Scanner) Scan(ctx context.Context, store memory.API) (int, error) {
	var ingested int
	for _, root := range s.Roots {
		if err := ctx.Err(); err != nil {
			return ingested, err
		}
		if root == "" {
			continue
		}
		if isOpenCodeRoot(root) {
			count, err := s.scanOpenCode(ctx, store)
			if err != nil {
				return ingested, err
			}
			ingested += count
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".jsonl" && ext != ".json" {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if s.IdleAfter > 0 && time.Since(info.ModTime()) < s.IdleAfter {
				return nil
			}
			if !s.NotBefore.IsZero() && info.ModTime().Before(s.NotBefore) {
				return nil
			}
			key := "transcript:" + path
			fingerprint := fileID(path, info)
			now := s.now()
			if !s.State.ShouldProcess(key, fingerprint, now) {
				return nil
			}
			transcript, err := parseTranscriptPath(path, info)
			if errors.Is(err, errNotTranscript) {
				return nil
			}
			if err != nil {
				if s.State != nil {
					s.State.MarkFailed(key, fingerprint, err, now)
				}
				return nil
			}
			if transcript.SourceMaterial == "" {
				return nil
			}
			records, err := StoreTranscriptMemories(ctx, store, s.Summarizer, s.MemoryContextLimit, transcript, info.ModTime().UTC())
			if err != nil {
				if s.State != nil {
					s.State.MarkFailed(key, fingerprint, err, now)
					return nil
				}
				return err
			}
			s.State.MarkProcessed(key, fingerprint, now)
			ingested += len(records)
			return nil
		})
		if err != nil {
			return ingested, err
		}
	}
	return ingested, nil
}

func (s Scanner) scanOpenCode(ctx context.Context, store memory.API) (int, error) {
	ids, err := openCodeSessionIDs(ctx)
	if err != nil {
		return 0, err
	}
	var ingested int
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return ingested, err
		}
		data, err := exportOpenCodeSession(ctx, id)
		if err != nil {
			return ingested, err
		}
		transcript, err := parseOpenCodeExport("opencode:"+id, data, time.Time{})
		if err != nil {
			continue
		}
		if s.IdleAfter > 0 && !transcript.Modified.IsZero() && s.now().Sub(transcript.Modified) < s.IdleAfter {
			continue
		}
		if !s.NotBefore.IsZero() && !transcript.Modified.IsZero() && transcript.Modified.Before(s.NotBefore) {
			continue
		}
		key := "opencode:" + id
		fingerprint := dataID(data)
		now := s.now()
		if !s.State.ShouldProcess(key, fingerprint, now) {
			continue
		}
		records, err := StoreTranscriptMemories(ctx, store, s.Summarizer, s.MemoryContextLimit, transcript, transcript.Modified.UTC())
		if err != nil {
			if s.State != nil {
				s.State.MarkFailed(key, fingerprint, err, now)
				continue
			}
			return ingested, err
		}
		s.State.MarkProcessed(key, fingerprint, now)
		ingested += len(records)
	}
	return ingested, nil
}

func (s Scanner) now() time.Time {
	if !s.Now.IsZero() {
		return s.Now
	}
	return time.Now()
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
	return parseTranscriptPath(path, info)
}

var errNotTranscript = errors.New("not a supported transcript")

func parseTranscriptPath(path string, info fs.FileInfo) (Transcript, error) {
	switch filepath.Ext(path) {
	case ".jsonl":
		return parseTranscript(path, info)
	case ".json":
		data, err := os.ReadFile(path)
		if err != nil {
			return Transcript{}, err
		}
		return parseOpenCodeExport(path, data, info.ModTime().UTC())
	default:
		return Transcript{}, errNotTranscript
	}
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

func StoreTranscriptMemories(ctx context.Context, store memory.API, agent summarizer.Agent, contextLimit int, transcript Transcript, now time.Time) ([]memory.Record, error) {
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
	return fmt.Sprintf("Transcript: %s\nCWD: %s\nModified: %s\nAssistant turns: %d\nTool calls: %d\nFirst user prompt: %s\nLast user prompt: %s\n\nRaw transcript data:\n%s",
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

func dataID(data []byte) string {
	h := sha1.Sum(data)
	return hex.EncodeToString(h[:])[:16]
}

func isOpenCodeRoot(root string) bool {
	if filepath.Base(root) != "opencode" {
		return false
	}
	_, err := os.Stat(filepath.Join(root, "opencode.db"))
	return err == nil
}

func openCodeSessionIDs(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "opencode", "session", "list")
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list opencode sessions: %w", err)
	}
	var ids []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "ses_") || seen[fields[0]] {
			continue
		}
		seen[fields[0]] = true
		ids = append(ids, fields[0])
	}
	return ids, nil
}

func exportOpenCodeSession(ctx context.Context, id string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "opencode", "export", id)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("export opencode session %s: %w", id, err)
	}
	return out, nil
}

func parseOpenCodeExport(path string, data []byte, fallbackModified time.Time) (Transcript, error) {
	var doc struct {
		Info struct {
			ID        string `json:"id"`
			Directory string `json:"directory"`
			Path      string `json:"path"`
			Time      struct {
				Created int64 `json:"created"`
				Updated int64 `json:"updated"`
			} `json:"time"`
		} `json:"info"`
		Messages []struct {
			Info struct {
				Role string `json:"role"`
				Path struct {
					CWD  string `json:"cwd"`
					Root string `json:"root"`
				} `json:"path"`
			} `json:"info"`
			Parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Tool string `json:"tool"`
			} `json:"parts"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Transcript{}, err
	}
	if doc.Info.ID == "" || len(doc.Messages) == 0 {
		return Transcript{}, errNotTranscript
	}

	cwd := doc.Info.Directory
	if cwd == "" {
		cwd = doc.Info.Path
	}
	var firstUser string
	var lastUser string
	var assistantTurns int
	var toolCalls int
	for _, message := range doc.Messages {
		if cwd == "" {
			cwd = message.Info.Path.CWD
		}
		text := openCodeText(message.Parts)
		switch message.Info.Role {
		case "user":
			if strings.TrimSpace(text) == "" {
				continue
			}
			if firstUser == "" {
				firstUser = strings.TrimSpace(text)
			}
			lastUser = strings.TrimSpace(text)
		case "assistant":
			assistantTurns++
		}
		for _, part := range message.Parts {
			if part.Type == "tool" || part.Tool != "" {
				toolCalls++
			}
		}
	}
	if firstUser == "" && lastUser == "" && assistantTurns == 0 && toolCalls == 0 {
		return Transcript{}, nil
	}
	modified := unixMillis(doc.Info.Time.Updated)
	if modified.IsZero() {
		modified = fallbackModified
	}
	if modified.IsZero() {
		modified = time.Now().UTC()
	}
	project := filepath.Base(cwd)
	if project == "." || project == "/" || project == "" {
		project = "opencode"
	}
	return Transcript{
		ID:             "opencode:" + doc.Info.ID,
		Project:        project,
		Path:           path,
		CWD:            cwd,
		Modified:       modified.UTC(),
		AssistantTurns: assistantTurns,
		ToolCalls:      toolCalls,
		FirstUser:      firstUser,
		LastUser:       lastUser,
		SourceMaterial: string(data),
	}, nil
}

func openCodeText(parts []struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Tool string `json:"tool"`
}) string {
	var texts []string
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func unixMillis(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value > 1_000_000_000_000 {
		return time.UnixMilli(value)
	}
	return time.Unix(value, 0)
}
