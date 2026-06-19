package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tomnagengast/agent-memoryd/internal/config"
	"github.com/tomnagengast/agent-memoryd/internal/ingest"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/summarizer"
	"github.com/tomnagengast/agent-memoryd/internal/version"
)

type searchInput struct {
	Query       string `json:"query" jsonschema:"natural language memory search query"`
	Project     string `json:"project,omitempty" jsonschema:"optional project scope filter"`
	Kind        string `json:"kind,omitempty" jsonschema:"optional memory kind filter, such as fact or feedback"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum number of results to return"`
	Diagnostics bool   `json:"diagnostics,omitempty" jsonschema:"include whether FTS and vector search participated"`
}

type searchOutput struct {
	Results     []memory.SearchResult     `json:"results"`
	Diagnostics *memory.SearchDiagnostics `json:"diagnostics,omitempty"`
}

type contextInput struct {
	Query    string `json:"query" jsonschema:"natural language memory search query"`
	Project  string `json:"project,omitempty" jsonschema:"optional project scope filter"`
	Kind     string `json:"kind,omitempty" jsonschema:"optional memory kind filter, such as fact or feedback"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum number of search results to expand"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"maximum total body characters to return across expanded memories"`
}

type contextOutput struct {
	Results   []contextEntry `json:"results"`
	MaxChars  int            `json:"max_chars"`
	Truncated bool           `json:"truncated"`
}

type contextEntry struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	Project       string  `json:"project,omitempty"`
	Source        string  `json:"source,omitempty"`
	Summary       string  `json:"summary"`
	Body          string  `json:"body"`
	BodyTruncated bool    `json:"body_truncated"`
	Score         float64 `json:"score"`
}

type getInput struct {
	ID string `json:"id" jsonschema:"memory id"`
}

type getOutput struct {
	Found  bool           `json:"found"`
	Memory *memory.Record `json:"memory,omitempty"`
}

type addInput struct {
	ID      string `json:"id,omitempty" jsonschema:"optional stable id for upsert"`
	Kind    string `json:"kind,omitempty" jsonschema:"memory kind, such as fact or feedback"`
	Project string `json:"project,omitempty" jsonschema:"optional project scope"`
	Source  string `json:"source,omitempty" jsonschema:"optional source reference"`
	Summary string `json:"summary,omitempty" jsonschema:"short memory summary"`
	Body    string `json:"body" jsonschema:"full memory body"`
}

type addOutput struct {
	OK     bool          `json:"ok"`
	Memory memory.Record `json:"memory"`
}

type forgetInput struct {
	ID string `json:"id" jsonschema:"memory id to delete"`
}

type forgetOutput struct {
	OK bool   `json:"ok"`
	ID string `json:"id"`
}

type reflectInput struct {
	Project        string `json:"project,omitempty" jsonschema:"optional project scope for generated memories"`
	CWD            string `json:"cwd,omitempty" jsonschema:"optional current working directory used to infer project"`
	TranscriptPath string `json:"transcript_path,omitempty" jsonschema:"optional explicit transcript JSONL path to reflect on"`
	Session        string `json:"session,omitempty" jsonschema:"optional current session text or transcript content; use when the client can provide the active session directly"`
	Source         string `json:"source,omitempty" jsonschema:"optional source reference for supplied session text"`
	Limit          int    `json:"limit,omitempty" jsonschema:"maximum number of existing memories to send as context"`
}

type reflectOutput struct {
	OK       bool            `json:"ok"`
	Source   string          `json:"source"`
	Memories []memory.Record `json:"memories"`
}

func runMCP(ctx context.Context, cfg config.Config, store memory.API) error {
	server := newMCPServer(cfg, store)
	return server.Run(ctx, &mcp.StdioTransport{})
}

func newMCPServer(cfg config.Config, store memory.API) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agent-memoryd",
		Version: version.Value(),
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "status",
		Description: "Return compact daemon store and embedder health.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, memory.Status, error) {
		status, err := store.Status(ctx)
		if err != nil {
			return nil, memory.Status{}, err
		}
		return nil, status, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "context",
		Description: "Search local agent memories and expand top hits into concise bounded context.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in contextInput) (*mcp.CallToolResult, contextOutput, error) {
		out, err := buildMemoryContext(ctx, store, in)
		if err != nil {
			return nil, contextOutput{}, err
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Search local agent memory summaries. Use get to expand a result only when needed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, searchOutput, error) {
		req := memory.SearchRequest{
			Query:   in.Query,
			Project: in.Project,
			Kind:    in.Kind,
			Limit:   in.Limit,
		}
		if in.Diagnostics {
			response, err := searchMemoryDetailed(ctx, store, req)
			if err != nil {
				return nil, searchOutput{}, err
			}
			return nil, searchOutput{Results: response.Results, Diagnostics: &response.Diagnostics}, nil
		}
		results, err := store.Search(ctx, req)
		if err != nil {
			return nil, searchOutput{}, err
		}
		return nil, searchOutput{Results: results}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get",
		Description: "Fetch one full memory by id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getInput) (*mcp.CallToolResult, getOutput, error) {
		record, err := store.Get(ctx, in.ID)
		if errors.Is(err, memory.ErrNotFound) {
			return nil, getOutput{Found: false}, nil
		}
		if err != nil {
			return nil, getOutput{}, err
		}
		return nil, getOutput{Found: true, Memory: &record}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add",
		Description: "Create or update a durable local memory.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in addInput) (*mcp.CallToolResult, addOutput, error) {
		record, err := store.Add(ctx, memory.AddRequest{
			ID:      in.ID,
			Kind:    in.Kind,
			Project: in.Project,
			Source:  in.Source,
			Summary: in.Summary,
			Body:    in.Body,
		})
		if err != nil {
			return nil, addOutput{}, err
		}
		return nil, addOutput{OK: true, Memory: record}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "forget",
		Description: "Delete a local memory by id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in forgetInput) (*mcp.CallToolResult, forgetOutput, error) {
		err := store.Forget(ctx, in.ID)
		if errors.Is(err, memory.ErrNotFound) {
			return nil, forgetOutput{OK: false, ID: in.ID}, nil
		}
		if err != nil {
			return nil, forgetOutput{}, err
		}
		return nil, forgetOutput{OK: true, ID: in.ID}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "reflect",
		Description: "Extract durable memories from the current session. Provide session text when available; otherwise this uses transcript_path or the newest configured transcript.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in reflectInput) (*mcp.CallToolResult, reflectOutput, error) {
		out, err := reflectMemories(ctx, cfg, store, in)
		if err != nil {
			return nil, reflectOutput{}, err
		}
		return nil, out, nil
	})

	return server
}

const (
	defaultContextLimit    = 5
	maxContextLimit        = 20
	defaultContextMaxChars = 6000
	maxContextMaxChars     = 20000
)

func buildMemoryContext(ctx context.Context, store memory.API, in contextInput) (contextOutput, error) {
	limit := normalizeContextLimit(in.Limit)
	maxChars := normalizeContextMaxChars(in.MaxChars)
	results, err := store.Search(ctx, memory.SearchRequest{
		Query:   in.Query,
		Project: in.Project,
		Kind:    in.Kind,
		Limit:   limit,
	})
	if err != nil {
		return contextOutput{}, err
	}

	entries := make([]contextEntry, 0, len(results))
	remaining := maxChars
	truncated := false
	for i, result := range results {
		record, err := store.Get(ctx, result.ID)
		if errors.Is(err, memory.ErrNotFound) {
			continue
		}
		if err != nil {
			return contextOutput{}, err
		}

		remainingResults := len(results) - i
		entryBudget := 0
		if remaining > 0 && remainingResults > 0 {
			entryBudget = remaining / remainingResults
			if entryBudget == 0 {
				entryBudget = remaining
			}
		}
		body, bodyTruncated := limitText(record.Body, entryBudget)
		if bodyTruncated {
			truncated = true
		}
		remaining -= runeCount(body)
		if remaining < 0 {
			remaining = 0
		}

		entries = append(entries, contextEntry{
			ID:            record.ID,
			Kind:          record.Kind,
			Project:       record.Project,
			Source:        record.Source,
			Summary:       record.Summary,
			Body:          body,
			BodyTruncated: bodyTruncated,
			Score:         result.Score,
		})
	}
	return contextOutput{Results: entries, MaxChars: maxChars, Truncated: truncated}, nil
}

func normalizeContextLimit(limit int) int {
	if limit <= 0 {
		return defaultContextLimit
	}
	if limit > maxContextLimit {
		return maxContextLimit
	}
	return limit
}

func normalizeContextMaxChars(maxChars int) int {
	if maxChars <= 0 {
		return defaultContextMaxChars
	}
	if maxChars > maxContextMaxChars {
		return maxContextMaxChars
	}
	return maxChars
}

func limitText(text string, maxChars int) (string, bool) {
	text = strings.TrimSpace(text)
	if maxChars <= 0 {
		return "", text != ""
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text, false
	}
	if maxChars <= 3 {
		return string(runes[:maxChars]), true
	}
	return string(runes[:maxChars-3]) + "...", true
}

func runeCount(text string) int {
	return len([]rune(text))
}

func reflectMemories(ctx context.Context, cfg config.Config, store memory.API, in reflectInput) (reflectOutput, error) {
	agent := summarizer.CommandAgent{
		Command: cfg.SummarizerCommand,
		Timeout: cfg.SummarizerTimeout,
	}
	limit := in.Limit
	if limit <= 0 {
		limit = cfg.MemoryContextLimit
	}
	if strings.TrimSpace(in.Session) != "" {
		records, err := reflectSessionText(ctx, store, agent, in, limit)
		if err != nil {
			return reflectOutput{}, err
		}
		return reflectOutput{OK: true, Source: sessionSource(in), Memories: records}, nil
	}

	path := strings.TrimSpace(in.TranscriptPath)
	if path == "" {
		latest, err := latestTranscript(cfg.TranscriptRoots)
		if err != nil {
			return reflectOutput{}, err
		}
		path = latest
	}
	transcript, err := ingest.ReadTranscript(path)
	if err != nil {
		return reflectOutput{}, err
	}
	if strings.TrimSpace(in.Project) != "" {
		transcript.Project = strings.TrimSpace(in.Project)
	}
	if strings.TrimSpace(in.CWD) != "" {
		transcript.CWD = strings.TrimSpace(in.CWD)
		if strings.TrimSpace(in.Project) == "" {
			transcript.Project = filepath.Base(transcript.CWD)
		}
	}
	records, err := ingest.StoreTranscriptMemories(ctx, store, agent, limit, transcript, time.Now().UTC())
	if err != nil {
		return reflectOutput{}, err
	}
	return reflectOutput{OK: true, Source: transcript.Path, Memories: records}, nil
}

func reflectSessionText(ctx context.Context, store memory.API, agent summarizer.Agent, in reflectInput, limit int) ([]memory.Record, error) {
	project := strings.TrimSpace(in.Project)
	if project == "" && strings.TrimSpace(in.CWD) != "" {
		project = filepath.Base(strings.TrimSpace(in.CWD))
	}
	source := sessionSource(in)
	detail := "Session: " + source
	existing, err := summarizer.ExistingMemoryRefs(ctx, store, project, limit)
	if err != nil {
		return nil, err
	}
	result, err := agent.Summarize(ctx, summarizer.Request{
		Producer:         "reflect",
		Project:          project,
		Source:           source,
		DetailReference:  detail,
		SourceMaterial:   sessionSourceMaterial(in),
		ExistingMemories: existing,
	})
	if err != nil {
		return nil, err
	}
	id := "reflect:" + stableTextID(source+"\n"+in.Session)
	records := make([]memory.Record, 0, len(result.Memories))
	for i, item := range result.Memories {
		kind := item.Kind
		if kind == "" {
			kind = "reflection"
		}
		body := summarizer.EnsureDetailReference(item.Body, detail)
		record, err := store.Add(ctx, memory.AddRequest{
			ID:      fmt.Sprintf("%s:%02d", id, i),
			Kind:    kind,
			Project: project,
			Source:  source,
			Summary: item.Summary,
			Body:    body,
		})
		if err != nil {
			return records, err
		}
		records = append(records, record)
	}
	return records, nil
}

func sessionSource(in reflectInput) string {
	if source := strings.TrimSpace(in.Source); source != "" {
		return source
	}
	if cwd := strings.TrimSpace(in.CWD); cwd != "" {
		return "mcp:reflect:" + cwd
	}
	return "mcp:reflect"
}

func sessionSourceMaterial(in reflectInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Source: %s\n", sessionSource(in))
	if project := strings.TrimSpace(in.Project); project != "" {
		fmt.Fprintf(&b, "Project: %s\n", project)
	}
	if cwd := strings.TrimSpace(in.CWD); cwd != "" {
		fmt.Fprintf(&b, "CWD: %s\n", cwd)
	}
	b.WriteString("\nCurrent session material:\n")
	b.WriteString(strings.TrimSpace(in.Session))
	b.WriteString("\n")
	return b.String()
}

func latestTranscript(roots []string) (string, error) {
	var latest string
	var latestMod time.Time
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
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
			if latest == "" || info.ModTime().After(latestMod) {
				latest = path
				latestMod = info.ModTime()
			}
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if latest == "" {
		return "", fmt.Errorf("no transcript JSONL files found in configured transcript roots")
	}
	return latest, nil
}

func stableTextID(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}
