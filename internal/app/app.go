package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: agent-memoryd <add|search|get|forget|mcp>")
	}
	root := dataRoot()
	store := memory.NewStore(filepath.Join(root, "memories.jsonl"))
	ctx := context.Background()

	switch args[0] {
	case "add":
		return runAdd(ctx, store, args[1:])
	case "search":
		return runSearch(ctx, store, args[1:])
	case "get":
		return runGet(ctx, store, args[1:])
	case "forget":
		return runForget(ctx, store, args[1:])
	case "mcp":
		return runMCP(ctx, store)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runAdd(ctx context.Context, store *memory.Store, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	id := fs.String("id", "", "memory id")
	kind := fs.String("kind", "fact", "memory kind")
	project := fs.String("project", "", "project scope")
	source := fs.String("source", "", "source reference")
	summary := fs.String("summary", "", "memory summary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: agent-memoryd add [flags] <body>")
	}
	record, err := store.Add(ctx, memory.AddRequest{
		ID:      *id,
		Kind:    *kind,
		Project: *project,
		Source:  *source,
		Summary: *summary,
		Body:    fs.Arg(0),
	})
	if err != nil {
		return err
	}
	return printJSON(record)
}

func runSearch(ctx context.Context, store *memory.Store, args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	kind := fs.String("kind", "", "memory kind")
	project := fs.String("project", "", "project scope")
	limit := fs.Int("limit", 5, "maximum result count")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: agent-memoryd search [flags] <query>")
	}
	results, err := store.Search(ctx, memory.SearchRequest{
		Query:   fs.Arg(0),
		Kind:    *kind,
		Project: *project,
		Limit:   *limit,
	})
	if err != nil {
		return err
	}
	return printJSON(results)
}

func runGet(ctx context.Context, store *memory.Store, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: agent-memoryd get <id>")
	}
	record, err := store.Get(ctx, args[0])
	if errors.Is(err, memory.ErrNotFound) {
		return printJSON(map[string]any{"found": false, "id": args[0]})
	}
	if err != nil {
		return err
	}
	return printJSON(record)
}

func runForget(ctx context.Context, store *memory.Store, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: agent-memoryd forget <id>")
	}
	err := store.Forget(ctx, args[0])
	if errors.Is(err, memory.ErrNotFound) {
		return printJSON(map[string]any{"ok": false, "id": args[0]})
	}
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"ok": true, "id": args[0]})
}

func dataRoot() string {
	if root := os.Getenv("AGENT_MEMORYD_HOME"); root != "" {
		return root
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return ".agent-memoryd"
	}
	return filepath.Join(dir, ".local", "share", "agent-memoryd")
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
