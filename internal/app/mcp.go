package app

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
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

func runMCP(ctx context.Context, store *memory.Store) error {
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

	return server.Run(ctx, &mcp.StdioTransport{})
}
