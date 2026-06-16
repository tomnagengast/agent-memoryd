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
)

type searchInput struct {
	Query   string `json:"query" jsonschema:"natural language memory search query"`
	Project string `json:"project,omitempty" jsonschema:"optional project scope filter"`
	Kind    string `json:"kind,omitempty" jsonschema:"optional memory kind filter, such as fact or feedback"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum number of results to return"`
}

type searchOutput struct {
	Results []memory.SearchResult `json:"results"`
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

func runMCP(ctx context.Context, cfg config.Config, store *memory.Store) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agent-memoryd",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Search local agent memory summaries. Use get to expand a result only when needed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, searchOutput, error) {
		results, err := store.Search(ctx, memory.SearchRequest{
			Query:   in.Query,
			Project: in.Project,
			Kind:    in.Kind,
			Limit:   in.Limit,
		})
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

	return server.Run(ctx, &mcp.StdioTransport{})
}

func reflectMemories(ctx context.Context, cfg config.Config, store *memory.Store, in reflectInput) (reflectOutput, error) {
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

func reflectSessionText(ctx context.Context, store *memory.Store, agent summarizer.Agent, in reflectInput, limit int) ([]memory.Record, error) {
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
