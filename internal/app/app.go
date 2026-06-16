package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tomnagengast/agent-memoryd/internal/config"
	"github.com/tomnagengast/agent-memoryd/internal/daemon"
	"github.com/tomnagengast/agent-memoryd/internal/indexer"
	"github.com/tomnagengast/agent-memoryd/internal/launchd"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/spool"
)

func Run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: agent-memoryd <init|status|uninstall|add|search|get|forget|reindex|mcp|daemon|scan-once|enqueue-git|launchd-plist>")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()

	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "status":
		return runStatus(ctx, cfg)
	case "uninstall":
		return runUninstall(cfg, args[1:])
	case "enqueue-git":
		return runEnqueueGit(cfg, args[1:])
	case "launchd-plist":
		return runLaunchdPlist(cfg, args[1:])
	}

	index, err := indexer.New(cfg)
	if err != nil {
		return err
	}
	store := memory.NewStoreWithIndex(cfg.StorePath, index)

	switch args[0] {
	case "add":
		return runAdd(ctx, store, args[1:])
	case "search":
		return runSearch(ctx, store, args[1:])
	case "get":
		return runGet(ctx, store, args[1:])
	case "forget":
		return runForget(ctx, store, args[1:])
	case "reindex":
		return runReindex(ctx, store)
	case "mcp":
		return runMCP(ctx, store)
	case "daemon":
		return runDaemon(cfg, store)
	case "scan-once":
		return runScanOnce(ctx, cfg, store)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	path := fs.String("path", "", "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, manifest, err := config.Init(*path)
	if err != nil {
		return err
	}
	out := config.ConfigPath(cfg.Root)
	if *path != "" {
		out = *path
	}
	return printJSON(map[string]any{
		"ok":        true,
		"config":    out,
		"manifest":  config.ManifestPath(cfg.Root),
		"resources": manifest.Resources,
	})
}

func runStatus(ctx context.Context, cfg config.Config) error {
	store := memory.NewStore(cfg.StorePath)
	status, err := store.Status(ctx)
	if err != nil {
		return err
	}
	status.Index = cfg.IndexBackend
	manifest, err := config.LoadManifest(cfg.Root)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"initialized": manifest.CreatedAt.IsZero() == false,
		"help":        systemHelp(),
		"config":      cfg,
		"store":       status,
		"resources":   manifest.Resources,
	})
}

func runUninstall(cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "remove all managed agent-memoryd resources")
	if err := fs.Parse(args); err != nil {
		return err
	}
	manifest, err := config.LoadManifest(cfg.Root)
	if err != nil {
		return err
	}
	if !*yes {
		return printJSON(map[string]any{
			"ok":         false,
			"needs_yes":  true,
			"message":    "rerun with --yes to remove managed agent-memoryd resources",
			"resources":  manifest.Resources,
			"help":       systemHelp(),
			"configured": cfg.Root,
		})
	}
	if err := config.Uninstall(cfg, manifest); err != nil {
		return err
	}
	return printJSON(map[string]any{"ok": true, "removed_root": cfg.Root})
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

func runReindex(ctx context.Context, store *memory.Store) error {
	if err := store.RebuildIndex(ctx); err != nil {
		return err
	}
	return printJSON(map[string]any{"ok": true})
}

func runDaemon(cfg config.Config, store *memory.Store) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	d := daemon.Daemon{
		Config: cfg,
		Store:  store,
		Log:    slog.Default(),
	}
	err := d.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func runScanOnce(ctx context.Context, cfg config.Config, store *memory.Store) error {
	d := daemon.Daemon{Config: cfg, Store: store}
	if err := d.Once(ctx); err != nil {
		return err
	}
	return printJSON(map[string]any{"ok": true})
}

func runEnqueueGit(cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("enqueue-git", flag.ContinueOnError)
	repo := fs.String("repo", "", "git repository path")
	sha := fs.String("sha", "HEAD", "commit sha")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repo == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		*repo = cwd
	}
	path, err := spool.EnqueueGit(cfg.SpoolDir, spool.GitEvent{Repo: *repo, SHA: *sha})
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"ok": true, "event": path})
}

func runLaunchdPlist(cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("launchd-plist", flag.ContinueOnError)
	bin := fs.String("bin", "", "agent-memoryd binary path")
	label := fs.String("label", "dev.agent-memoryd", "launchd label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		*bin = exe
	}
	text, err := launchd.Render(launchd.Config{
		Label:  *label,
		Binary: *bin,
		Root:   cfg.Root,
		LogDir: filepath.Join(cfg.Root, "logs"),
	})
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func systemHelp() map[string]any {
	return map[string]any{
		"commands": []map[string]string{
			{"name": "init", "summary": "create config, data directories, memory store, and resource manifest"},
			{"name": "status", "summary": "show help, config, store status, and managed resources"},
			{"name": "uninstall --yes", "summary": "remove managed agent-memoryd resources"},
			{"name": "mcp", "summary": "run the MCP stdio server"},
			{"name": "daemon", "summary": "run the resident ingest worker"},
			{"name": "scan-once", "summary": "process git spool events and idle transcripts once"},
			{"name": "enqueue-git", "summary": "enqueue a git commit summary event"},
			{"name": "launchd-plist", "summary": "render a macOS LaunchAgent plist"},
			{"name": "add", "summary": "create or update a memory"},
			{"name": "search", "summary": "search memory summaries"},
			{"name": "get", "summary": "fetch one full memory"},
			{"name": "forget", "summary": "delete one memory"},
			{"name": "reindex", "summary": "rebuild the configured retrieval index from the source store"},
		},
		"mcp_tools": []map[string]string{
			{"name": "search", "summary": "search local memory summaries"},
			{"name": "get", "summary": "fetch one full memory by id"},
			{"name": "add", "summary": "create or update a durable memory"},
			{"name": "forget", "summary": "delete a memory by id"},
		},
	}
}
