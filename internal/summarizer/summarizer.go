package summarizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

type Agent interface {
	Summarize(context.Context, Request) (Result, error)
}

type CommandAgent struct {
	Command []string
	Timeout time.Duration
}

type Request struct {
	Producer         string
	Project          string
	Source           string
	DetailReference  string
	SourceMaterial   string
	ExistingMemories []MemoryRef
}

type MemoryRef struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty"`
	Source  string `json:"source,omitempty"`
	Summary string `json:"summary"`
}

type Result struct {
	Memories []GeneratedMemory `json:"memories"`
}

type GeneratedMemory struct {
	Kind    string `json:"kind,omitempty"`
	Summary string `json:"summary"`
	Body    string `json:"body"`
}

func (a CommandAgent) Summarize(ctx context.Context, req Request) (Result, error) {
	if len(a.Command) == 0 || strings.TrimSpace(a.Command[0]) == "" {
		return Result{}, fmt.Errorf("summarizer command is empty")
	}
	if a.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.Timeout)
		defer cancel()
	}
	prompt, err := buildPrompt(req)
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, a.Command[0], a.Command[1:]...)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("run summarizer: %w: subprocess output redacted (stdout_bytes=%d stderr_bytes=%d)", err, stdout.Len(), stderr.Len())
	}
	result, err := ParseResult(stdout.String())
	if err != nil {
		return Result{}, err
	}
	for i := range result.Memories {
		result.Memories[i].Body = EnsureDetailReference(result.Memories[i].Body, req.DetailReference)
	}
	return result, nil
}

func ExistingMemoryRefs(ctx context.Context, store *memory.Store, project string, limit int) ([]MemoryRef, error) {
	records, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	if limit <= 0 {
		limit = 12
	}
	refs := make([]MemoryRef, 0, limit)
	for _, record := range records {
		if project != "" && record.Project != "" && record.Project != project {
			continue
		}
		refs = append(refs, MemoryRef{
			ID:      record.ID,
			Kind:    record.Kind,
			Project: record.Project,
			Source:  record.Source,
			Summary: record.Summary,
		})
		if len(refs) >= limit {
			break
		}
	}
	return refs, nil
}

func ParseResult(text string) (Result, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Result{}, nil
	}
	trimmed = extractJSONObject(trimmed)
	var result Result
	if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
		return Result{}, fmt.Errorf("decode summarizer JSON: %w", err)
	}
	filtered := result.Memories[:0]
	for _, item := range result.Memories {
		item.Summary = strings.TrimSpace(item.Summary)
		item.Body = strings.TrimSpace(item.Body)
		item.Kind = strings.TrimSpace(item.Kind)
		if item.Body == "" {
			continue
		}
		if item.Summary == "" {
			item.Summary = memory.Summarize(item.Body, 180)
		}
		filtered = append(filtered, item)
	}
	result.Memories = filtered
	return result, nil
}

func buildPrompt(req Request) (string, error) {
	contextJSON, err := json.MarshalIndent(req.ExistingMemories, "", "  ")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("You are producing durable memories for coding agents.\n")
	b.WriteString("Do not make any modifications to files or external systems.\n")
	b.WriteString("Compare the source material against the existing memory summaries and return only new durable information worth remembering.\n")
	b.WriteString("Good memories include user preferences, standing instructions, project decisions, durable project facts, and follow-up context that will help a future agent.\n")
	b.WriteString("Do not copy raw transcript lines, tool logs, diffs, or git output into the memory body. Store concise distilled information only.\n")
	b.WriteString("Every memory body must include where to find more detail using the provided detail reference.\n")
	b.WriteString("Return JSON only with this shape: {\"memories\":[{\"kind\":\"preference|instruction|fact|session|git-summary\",\"summary\":\"short summary\",\"body\":\"concise durable memory\"}]}.\n")
	b.WriteString("Return {\"memories\":[]} if nothing new should be remembered.\n\n")
	fmt.Fprintf(&b, "Producer: %s\n", req.Producer)
	fmt.Fprintf(&b, "Project: %s\n", req.Project)
	fmt.Fprintf(&b, "Source: %s\n", req.Source)
	fmt.Fprintf(&b, "Detail reference: %s\n\n", req.DetailReference)
	b.WriteString("Existing memory summaries:\n")
	b.Write(contextJSON)
	b.WriteString("\n\nSource material:\n")
	b.WriteString(req.SourceMaterial)
	b.WriteString("\n")
	return b.String(), nil
}

func extractJSONObject(text string) string {
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) >= 3 {
			text = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end >= start {
		return text[start : end+1]
	}
	return text
}

func EnsureDetailReference(body, detail string) string {
	body = strings.TrimSpace(body)
	detail = strings.TrimSpace(detail)
	if body == "" || detail == "" || strings.Contains(body, detail) {
		return body
	}
	return body + "\n\nMore detail: " + detail
}
