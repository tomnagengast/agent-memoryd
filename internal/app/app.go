package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"
	"github.com/tomnagengast/agent-memoryd/internal/config"
	"github.com/tomnagengast/agent-memoryd/internal/daemon"
	"github.com/tomnagengast/agent-memoryd/internal/explore"
	"github.com/tomnagengast/agent-memoryd/internal/githooks"
	"github.com/tomnagengast/agent-memoryd/internal/importmem"
	"github.com/tomnagengast/agent-memoryd/internal/indexer"
	"github.com/tomnagengast/agent-memoryd/internal/launchd"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/spool"
	"github.com/tomnagengast/agent-memoryd/internal/version"
)

func Run(args []string) error {
	cmd := newRootCommand()
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		if isUsageError(err) {
			return fmt.Errorf("%w\n\nRun 'agent-memoryd --help' for usage.", err)
		}
		return err
	}
	return nil
}

func isUsageError(err error) bool {
	text := err.Error()
	return strings.HasPrefix(text, "unknown command") ||
		strings.HasPrefix(text, "unknown flag") ||
		strings.HasPrefix(text, "accepts ") ||
		strings.Contains(text, " accepts ") ||
		strings.Contains(text, " requires ")
}

func newRootCommand() *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:           "agent-memoryd",
		Short:         "Local memory daemon for coding agents.",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				fmt.Fprintln(cmd.OutOrStdout(), version.String())
				return nil
			}
			return cmd.Help()
		},
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "print version information")
	root.AddCommand(
		newInitCommand(),
		newStatusCommand(),
		newUninstallCommand(),
		newAddCommand(),
		newSearchCommand(),
		newGetCommand(),
		newForgetCommand(),
		newExploreCommand(),
		newReindexCommand(),
		newMCPCommand(),
		newDaemonCommand(),
		newScanOnceCommand(),
		newEnqueueGitCommand(),
		newLaunchdPlistCommand(),
	)
	return root
}

func newInitCommand() *cobra.Command {
	var path string
	var noDaemon bool
	var fresh bool
	var importPath string
	var importProject string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create config, choose memory import mode, install hooks, and start the daemon.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if fresh && importPath != "" {
				return fmt.Errorf("--fresh and --import cannot be used together")
			}
			cfg, manifest, err := config.Init(path)
			if err != nil {
				return err
			}
			store := memory.NewStore(cfg.StorePath)
			memoryImport, err := runInitMemoryImport(cmd.Context(), store, initMemoryImportOptions{
				Fresh:         fresh,
				ImportPath:    importPath,
				ImportProject: importProject,
			})
			if err != nil {
				return err
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			gitHooks, err := githooks.InstallManaged(cfg, exe)
			if err != nil {
				return err
			}
			manifest, err = config.LoadManifest(cfg.Root)
			if err != nil {
				return err
			}
			var service any = map[string]any{"started": false, "skipped": "disabled by --no-daemon"}
			if !noDaemon {
				status, err := launchd.InstallAndStart(launchd.Config{
					Label:     "dev.agent-memoryd",
					Binary:    exe,
					Root:      cfg.Root,
					LogDir:    filepath.Join(cfg.Root, "logs"),
					PlistPath: config.LaunchdPlistPath(),
				})
				if err != nil {
					return err
				}
				service = status
				manifest, err = config.LoadManifest(cfg.Root)
				if err != nil {
					return err
				}
			}
			out := config.ConfigPath(cfg.Root)
			if path != "" {
				out = path
			}
			return printJSON(map[string]any{
				"ok":            true,
				"config":        out,
				"manifest":      config.ManifestPath(cfg.Root),
				"resources":     manifest.Resources,
				"service":       service,
				"git_hooks":     gitHooks,
				"memory_import": memoryImport,
			})
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "config path")
	cmd.Flags().BoolVar(&noDaemon, "no-daemon", false, "do not install or start the launchd daemon")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "start with an empty memory store and do not prompt for imports")
	cmd.Flags().StringVar(&importPath, "import", "", "import existing memories from a JSONL file or markdown/text directory")
	cmd.Flags().StringVar(&importProject, "import-project", "", "project scope for imported markdown/text memories")
	return cmd
}

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show help, config, store status, and managed resources.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			return runStatus(cmd.Context(), cfg)
		},
	}
}

func newUninstallCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove managed agent-memoryd resources.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			return runUninstall(cfg, yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "remove all managed agent-memoryd resources")
	return cmd
}

func newAddCommand() *cobra.Command {
	var req memory.AddRequest
	cmd := &cobra.Command{
		Use:   "add [flags] <body>",
		Short: "Create or update a memory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := loadIndexedStore()
			if err != nil {
				return err
			}
			req.Body = args[0]
			record, err := store.Add(cmd.Context(), req)
			if err != nil {
				return err
			}
			return printJSON(record)
		},
	}
	cmd.Flags().StringVar(&req.ID, "id", "", "memory id")
	cmd.Flags().StringVar(&req.Kind, "kind", "fact", "memory kind")
	cmd.Flags().StringVar(&req.Project, "project", "", "project scope")
	cmd.Flags().StringVar(&req.Source, "source", "", "source reference")
	cmd.Flags().StringVar(&req.Summary, "summary", "", "memory summary")
	return cmd
}

func newSearchCommand() *cobra.Command {
	var req memory.SearchRequest
	cmd := &cobra.Command{
		Use:   "search [flags] <query>",
		Short: "Search memory summaries.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := loadIndexedStore()
			if err != nil {
				return err
			}
			req.Query = args[0]
			results, err := store.Search(cmd.Context(), req)
			if err != nil {
				return err
			}
			return printJSON(results)
		},
	}
	cmd.Flags().StringVar(&req.Kind, "kind", "", "memory kind")
	cmd.Flags().StringVar(&req.Project, "project", "", "project scope")
	cmd.Flags().IntVar(&req.Limit, "limit", 5, "maximum result count")
	return cmd
}

func newGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Fetch one full memory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := loadIndexedStore()
			if err != nil {
				return err
			}
			record, err := store.Get(cmd.Context(), args[0])
			if errors.Is(err, memory.ErrNotFound) {
				return printJSON(map[string]any{"found": false, "id": args[0]})
			}
			if err != nil {
				return err
			}
			return printJSON(record)
		},
	}
}

func newForgetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <id>",
		Short: "Delete one memory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := loadIndexedStore()
			if err != nil {
				return err
			}
			err = store.Forget(cmd.Context(), args[0])
			if errors.Is(err, memory.ErrNotFound) {
				return printJSON(map[string]any{"ok": false, "id": args[0]})
			}
			if err != nil {
				return err
			}
			return printJSON(map[string]any{"ok": true, "id": args[0]})
		},
	}
}

func newExploreCommand() *cobra.Command {
	var opts explore.Options
	cmd := &cobra.Command{
		Use:   "explore",
		Short: "Explore memories in an interactive TUI.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := loadIndexedStore()
			if err != nil {
				return err
			}
			return explore.Run(cmd.Context(), store, opts)
		},
	}
	cmd.Flags().IntVar(&opts.Limit, "limit", 100, "maximum memories to show")
	return cmd
}

func newReindexCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the configured retrieval index from the source store.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := loadIndexedStore()
			if err != nil {
				return err
			}
			if err := store.RebuildIndex(cmd.Context()); err != nil {
				return err
			}
			return printJSON(map[string]any{"ok": true})
		},
	}
}

func newMCPCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP stdio server.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, store, err := loadIndexedStore()
			if err != nil {
				return err
			}
			return runMCP(cmd.Context(), cfg, store)
		},
	}
}

func newDaemonCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the resident ingest worker.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, store, err := loadIndexedStore()
			if err != nil {
				return err
			}
			return runDaemon(cfg, store)
		},
	}
}

func newScanOnceCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "scan-once",
		Short: "Process git spool events and idle transcripts once.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, store, err := loadIndexedStore()
			if err != nil {
				return err
			}
			return runScanOnce(cmd.Context(), cfg, store)
		},
	}
}

func newEnqueueGitCommand() *cobra.Command {
	var repo string
	var sha string
	cmd := &cobra.Command{
		Use:   "enqueue-git",
		Short: "Enqueue a git commit summary event.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if repo == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				repo = cwd
			}
			path, err := spool.EnqueueGit(cfg.SpoolDir, spool.GitEvent{Repo: repo, SHA: sha})
			if err != nil {
				return err
			}
			return printJSON(map[string]any{"ok": true, "event": path})
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "git repository path")
	cmd.Flags().StringVar(&sha, "sha", "HEAD", "commit sha")
	return cmd
}

func newLaunchdPlistCommand() *cobra.Command {
	var bin string
	var label string
	cmd := &cobra.Command{
		Use:   "launchd-plist",
		Short: "Render a macOS LaunchAgent plist.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if bin == "" {
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				bin = exe
			}
			text, err := launchd.Render(launchd.Config{
				Label:  label,
				Binary: bin,
				Root:   cfg.Root,
				LogDir: filepath.Join(cfg.Root, "logs"),
			})
			if err != nil {
				return err
			}
			fmt.Print(text)
			return nil
		},
	}
	cmd.Flags().StringVar(&bin, "bin", "", "agent-memoryd binary path")
	cmd.Flags().StringVar(&label, "label", "dev.agent-memoryd", "launchd label")
	return cmd
}

func loadIndexedStore() (config.Config, *memory.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, nil, err
	}
	index, err := indexer.New(cfg)
	if err != nil {
		return config.Config{}, nil, err
	}
	return cfg, memory.NewStoreWithIndex(cfg.StorePath, index), nil
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
		"service": launchd.CurrentStatus(launchd.Config{
			Label:     "dev.agent-memoryd",
			PlistPath: config.LaunchdPlistPath(),
		}),
		"git_hooks": githooks.CurrentStatus(cfg),
		"resources": manifest.Resources,
	})
}

func runUninstall(cfg config.Config, yes bool) error {
	manifest, err := config.LoadManifest(cfg.Root)
	if err != nil {
		return err
	}
	if !yes {
		return printJSON(map[string]any{
			"ok":         false,
			"needs_yes":  true,
			"message":    "rerun with --yes to remove managed agent-memoryd resources",
			"resources":  manifest.Resources,
			"help":       systemHelp(),
			"configured": cfg.Root,
		})
	}
	if err := githooks.UninstallManaged(cfg); err != nil {
		return err
	}
	if err := config.Uninstall(cfg, manifest); err != nil {
		return err
	}
	return printJSON(map[string]any{"ok": true, "removed_root": cfg.Root})
}

type initMemoryImportOptions struct {
	Fresh         bool
	ImportPath    string
	ImportProject string
}

type initMemoryImportStatus struct {
	Mode     string            `json:"mode"`
	Result   *importmem.Result `json:"result,omitempty"`
	Skipped  string            `json:"skipped,omitempty"`
	Existing int               `json:"existing,omitempty"`
}

func runInitMemoryImport(ctx context.Context, store *memory.Store, opts initMemoryImportOptions) (initMemoryImportStatus, error) {
	if opts.ImportPath != "" {
		result, err := importmem.Import(ctx, store, importmem.Options{Path: opts.ImportPath, Project: opts.ImportProject})
		if err != nil {
			return initMemoryImportStatus{}, err
		}
		return initMemoryImportStatus{Mode: "import", Result: &result}, nil
	}
	status, err := store.Status(ctx)
	if err != nil {
		return initMemoryImportStatus{}, err
	}
	if opts.Fresh {
		return initMemoryImportStatus{Mode: "fresh", Existing: status.MemoryCount}, nil
	}
	if status.MemoryCount > 0 {
		return initMemoryImportStatus{Mode: "existing-store", Existing: status.MemoryCount, Skipped: "memory store already has records"}, nil
	}
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return initMemoryImportStatus{Mode: "fresh", Skipped: "non-interactive default"}, nil
	}
	choice, err := promptMemoryImport()
	if err != nil {
		return initMemoryImportStatus{}, err
	}
	if choice.ImportPath == "" {
		return initMemoryImportStatus{Mode: "fresh"}, nil
	}
	result, err := importmem.Import(ctx, store, importmem.Options{Path: choice.ImportPath, Project: choice.ImportProject})
	if err != nil {
		return initMemoryImportStatus{}, err
	}
	return initMemoryImportStatus{Mode: "import", Result: &result}, nil
}

type memoryImportChoice struct {
	ImportPath    string
	ImportProject string
}

func promptMemoryImport() (memoryImportChoice, error) {
	mode := "fresh"
	if err := huh.NewSelect[string]().
		Title("Memory setup").
		Options(
			huh.NewOption("Start fresh", "fresh"),
			huh.NewOption("Import existing memories", "import"),
		).
		Value(&mode).
		Run(); err != nil {
		return memoryImportChoice{}, err
	}
	if mode != "import" {
		return memoryImportChoice{}, nil
	}
	var choice memoryImportChoice
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Import path").
			Description("JSONL file, markdown file, text file, or directory").
			Placeholder("~/notes/agent").
			Value(&choice.ImportPath).
			Validate(func(value string) error {
				value = strings.TrimSpace(value)
				if value == "" {
					return fmt.Errorf("path is required")
				}
				if _, err := os.Stat(expandPath(value)); err != nil {
					return err
				}
				return nil
			}),
		huh.NewInput().
			Title("Project for text memories").
			Description("Optional; JSONL records keep their own project values").
			Value(&choice.ImportProject),
	)).Run(); err != nil {
		return memoryImportChoice{}, err
	}
	choice.ImportPath = strings.TrimSpace(choice.ImportPath)
	choice.ImportProject = strings.TrimSpace(choice.ImportProject)
	return choice, nil
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return os.ExpandEnv(path)
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

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func systemHelp() map[string]any {
	return map[string]any{
		"commands":  helpItemsJSON(commandHelp),
		"mcp_tools": helpItemsJSON(mcpToolHelp),
	}
}

type helpItem struct {
	Name    string
	Summary string
}

var commandHelp = []helpItem{
	{Name: "init", Summary: "create config, choose memory import mode, install hooks, and start the daemon"},
	{Name: "status", Summary: "show help, config, store status, and managed resources"},
	{Name: "uninstall --yes", Summary: "remove managed agent-memoryd resources"},
	{Name: "help [command]", Summary: "show command help"},
	{Name: "completion", Summary: "generate shell completion scripts"},
	{Name: "mcp", Summary: "run the MCP stdio server"},
	{Name: "daemon", Summary: "run the resident ingest worker"},
	{Name: "scan-once", Summary: "process git spool events and idle transcripts once"},
	{Name: "enqueue-git", Summary: "enqueue a git commit summary event"},
	{Name: "launchd-plist", Summary: "render a macOS LaunchAgent plist"},
	{Name: "add", Summary: "create or update a memory"},
	{Name: "search", Summary: "search memory summaries"},
	{Name: "get", Summary: "fetch one full memory"},
	{Name: "forget", Summary: "delete one memory"},
	{Name: "explore", Summary: "explore memories in an interactive TUI"},
	{Name: "reindex", Summary: "rebuild the configured retrieval index from the source store"},
}

var mcpToolHelp = []helpItem{
	{Name: "search", Summary: "search local memory summaries"},
	{Name: "get", Summary: "fetch one full memory by id"},
	{Name: "add", Summary: "create or update a durable memory"},
	{Name: "forget", Summary: "delete a memory by id"},
	{Name: "reflect", Summary: "extract durable memories from the current session"},
}

func helpItemsJSON(items []helpItem) []map[string]string {
	out := make([]map[string]string, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]string{"name": item.Name, "summary": item.Summary})
	}
	return out
}
