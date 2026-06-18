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
	"time"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"
	"github.com/tomnagengast/agent-memoryd/internal/config"
	"github.com/tomnagengast/agent-memoryd/internal/daemon"
	"github.com/tomnagengast/agent-memoryd/internal/embedder"
	"github.com/tomnagengast/agent-memoryd/internal/explore"
	"github.com/tomnagengast/agent-memoryd/internal/githooks"
	"github.com/tomnagengast/agent-memoryd/internal/importmem"
	"github.com/tomnagengast/agent-memoryd/internal/launchd"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/spool"
	"github.com/tomnagengast/agent-memoryd/internal/storerpc"
	"github.com/tomnagengast/agent-memoryd/internal/version"
)

func Run(args []string) error {
	cmd := newRootCommand()
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		if isUsageError(err) {
			return fmt.Errorf("%w\n\nRun '%s --help' for usage.", err, version.CommandName)
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
		Use:           version.CommandName,
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
			onboarding := defaultInitOnboarding(fresh, importPath, importProject, noDaemon)
			if shouldPromptInitOnboarding(cmd) {
				var err error
				onboarding, err = promptInitOnboarding(onboarding)
				if err != nil {
					return err
				}
			}
			cfg, manifest, err := config.InitWithConfig(path, onboarding.Config(config.Default()))
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
			memoryOpts := onboarding.MemoryImportOptions()
			var memoryImport initMemoryImportStatus
			var service any = map[string]any{"started": false, "skipped": "disabled by init choice"}
			if !onboarding.StartDaemon {
				memoryImport, err = runInitMemoryImportWithoutDaemon(memoryOpts)
				if err != nil {
					return err
				}
			} else {
				status, err := launchd.InstallAndStart(launchd.Config{
					Label:     launchd.DefaultLabel,
					Binary:    exe,
					Root:      cfg.Root,
					LogDir:    filepath.Join(cfg.Root, "logs"),
					PlistPath: config.LaunchdPlistPath(),
				})
				if err != nil {
					return err
				}
				service = status
				if err := waitForDaemon(cmd.Context(), cfg); err != nil {
					return err
				}
				_, store, err := dialOrOpen()
				if err != nil {
					return err
				}
				defer store.Close()
				memoryImport, err = runInitMemoryImport(cmd.Context(), store, memoryOpts)
				if err != nil {
					return err
				}
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
				"onboarding":    onboarding.Status(),
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
		Short: "Remove managed local memory resources.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			return runUninstall(cfg, yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "remove all managed local memory resources")
	return cmd
}

func newAddCommand() *cobra.Command {
	var req memory.AddRequest
	cmd := &cobra.Command{
		Use:   "add [flags] <body>",
		Short: "Create or update a memory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := dialOrOpen()
			if err != nil {
				return err
			}
			defer store.Close()
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
			_, store, err := dialOrOpen()
			if err != nil {
				return err
			}
			defer store.Close()
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
			_, store, err := dialOrOpen()
			if err != nil {
				return err
			}
			defer store.Close()
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
			_, store, err := dialOrOpen()
			if err != nil {
				return err
			}
			defer store.Close()
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
			_, store, err := dialOrOpen()
			if err != nil {
				return err
			}
			defer store.Close()
			return explore.Run(cmd.Context(), store, opts)
		},
	}
	cmd.Flags().IntVar(&opts.Limit, "limit", 100, "maximum memories to show")
	return cmd
}

func newReindexCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reindex",
		Short: "Embed memories that are missing vectors.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, store, err := dialOrOpen()
			if err != nil {
				return err
			}
			defer store.Close()
			count, err := store.Backfill(cmd.Context())
			if err != nil {
				return err
			}
			return printJSON(map[string]any{"ok": true, "embedded": count})
		},
	}
}

func newMCPCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP stdio server.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, store, err := dialOrOpen()
			if err != nil {
				return err
			}
			defer store.Close()
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
			cfg, store, err := loadStore()
			if err != nil {
				return err
			}
			defer store.Close()
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
			cfg, store, err := dialOrOpen()
			if err != nil {
				return err
			}
			defer store.Close()
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
	cmd.Flags().StringVar(&bin, "bin", "", "memoryd binary path")
	cmd.Flags().StringVar(&label, "label", launchd.DefaultLabel, "launchd label")
	return cmd
}

// loadStore opens the zvec store directly.  Used only by the daemon command,
// which is the exclusive process-level owner of the collection.
func loadStore() (config.Config, *memory.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, nil, err
	}
	var emb embedder.Embedder = embedder.Disabled{}
	if len(cfg.EmbedderCommand) > 0 {
		emb = embedder.Command{
			Argv:    cfg.EmbedderCommand,
			Timeout: cfg.EmbedderTimeout,
		}
	}
	store, err := memory.Open(memory.OpenConfig{
		ZvecPath:     cfg.ZvecPath,
		EmbeddingDim: cfg.EmbeddingDim,
		LockTimeout:  cfg.LockTimeout,
		FTSWeight:    cfg.SearchFTSWeight,
		VectorWeight: cfg.SearchVectorWeight,
		Embedder:     emb,
	})
	if err != nil {
		return config.Config{}, nil, err
	}
	return cfg, store, nil
}

var errDaemonNotRunning = errors.New("memoryd daemon is not running")

// dialOrOpen returns an RPC client for the daemon-owned store.  The daemon is
// the only process that may open zvec directly.
func dialOrOpen() (config.Config, memory.API, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, nil, err
	}
	if storerpc.Probe(cfg) {
		return cfg, storerpc.NewClient(cfg), nil
	}
	return config.Config{}, nil, fmt.Errorf("%w; start it with `memoryd daemon` or run `memoryd init`", errDaemonNotRunning)
}

func runStatus(ctx context.Context, cfg config.Config) error {
	_, store, err := dialOrOpen()
	if err != nil {
		return err
	}
	defer store.Close()
	status, err := store.Status(ctx)
	if err != nil {
		return err
	}
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
			Label:     launchd.DefaultLabel,
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
			"message":    "rerun with --yes to remove managed local memory resources",
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

type initOnboarding struct {
	Interactive    bool
	MemoryMode     string
	ImportPath     string
	ImportProject  string
	StartDaemon    bool
	TranscriptMode string
}

func defaultInitOnboarding(fresh bool, importPath, importProject string, noDaemon bool) initOnboarding {
	memoryMode := "prompt"
	if fresh {
		memoryMode = "fresh"
	}
	if importPath != "" {
		memoryMode = "import"
	}
	return initOnboarding{
		MemoryMode:     memoryMode,
		ImportPath:     importPath,
		ImportProject:  importProject,
		StartDaemon:    !noDaemon,
		TranscriptMode: "default",
	}
}

func shouldPromptInitOnboarding(cmd *cobra.Command) bool {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return false
	}
	for _, flag := range []string{"fresh", "import", "import-project", "no-daemon"} {
		if cmd.Flags().Changed(flag) {
			return false
		}
	}
	return true
}

func (o initOnboarding) Config(cfg config.Config) config.Config {
	if o.TranscriptMode == "disabled" {
		cfg.TranscriptRoots = []string{}
	}
	return cfg
}

func (o initOnboarding) MemoryImportOptions() initMemoryImportOptions {
	return initMemoryImportOptions{
		Fresh:         o.MemoryMode == "fresh",
		ImportPath:    o.ImportPath,
		ImportProject: o.ImportProject,
	}
}

func (o initOnboarding) Status() map[string]any {
	memoryMode := o.MemoryMode
	if memoryMode == "prompt" {
		memoryMode = "fresh"
	}
	return map[string]any{
		"interactive":      o.Interactive,
		"memory_mode":      memoryMode,
		"start_daemon":     o.StartDaemon,
		"transcript_roots": o.TranscriptMode,
	}
}

func runInitMemoryImport(ctx context.Context, store memory.API, opts initMemoryImportOptions) (initMemoryImportStatus, error) {
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
	return initMemoryImportStatus{Mode: "fresh"}, nil
}

func runInitMemoryImportWithoutDaemon(opts initMemoryImportOptions) (initMemoryImportStatus, error) {
	if opts.ImportPath != "" {
		return initMemoryImportStatus{}, fmt.Errorf("--import requires the daemon; remove --no-daemon or run memoryd daemon first")
	}
	status := initMemoryImportStatus{Mode: "fresh", Skipped: "daemon disabled by --no-daemon"}
	if opts.Fresh {
		status.Skipped = ""
	}
	return status, nil
}

func promptInitOnboarding(initial initOnboarding) (initOnboarding, error) {
	choice := initial
	choice.Interactive = true
	if choice.MemoryMode == "prompt" {
		choice.MemoryMode = "fresh"
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Memory setup").
			Description("Start empty or import JSONL, markdown, or text memories after the daemon starts.").
			Options(
				huh.NewOption("Start fresh", "fresh"),
				huh.NewOption("Import existing memories", "import"),
			).
			Value(&choice.MemoryMode),
		huh.NewSelect[string]().
			Title("Transcript ingestion").
			Description("The daemon can summarize idle Claude, Codex, and opencode transcripts.").
			Options(
				huh.NewOption("Enable default transcript roots", "default"),
				huh.NewOption("Disable transcript ingestion", "disabled"),
			).
			Value(&choice.TranscriptMode),
		huh.NewConfirm().
			Title("Start the background daemon now?").
			Description("The daemon owns zvec and serves CLI/MCP store operations over the local socket.").
			Affirmative("Start daemon").
			Negative("Skip service").
			Value(&choice.StartDaemon),
	))
	applyHuhOptions(form)
	if err := form.Run(); err != nil {
		return initOnboarding{}, err
	}
	if choice.MemoryMode == "import" && !choice.StartDaemon {
		return initOnboarding{}, fmt.Errorf("import requires the daemon; choose fresh setup or start the background daemon")
	}
	if choice.MemoryMode != "import" {
		choice.ImportPath = ""
		choice.ImportProject = ""
		return choice, nil
	}
	importForm := huh.NewForm(huh.NewGroup(
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
	))
	applyHuhOptions(importForm)
	if err := importForm.Run(); err != nil {
		return initOnboarding{}, err
	}
	choice.ImportPath = strings.TrimSpace(choice.ImportPath)
	choice.ImportProject = strings.TrimSpace(choice.ImportProject)
	return choice, nil
}

func applyHuhOptions(form *huh.Form) {
	if os.Getenv("MEMORYD_ACCESSIBLE") != "" || os.Getenv("ACCESSIBLE") != "" {
		form.WithAccessible(true)
	}
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

func waitForDaemon(ctx context.Context, cfg config.Config) error {
	timeout := cfg.LockTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if storerpc.Probe(cfg) {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("%w; socket %s did not become available", errDaemonNotRunning, storerpc.SocketPath(cfg))
		case <-ticker.C:
		}
	}
}

func runDaemon(cfg config.Config, store *memory.Store) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start the RPC server so CLI/MCP processes can talk to us via the socket
	// instead of opening zvec directly (zvec takes a fully-exclusive lock).
	srv := storerpc.NewServer(store)
	ln, err := srv.Listen(cfg)
	if err != nil {
		return fmt.Errorf("start rpc server: %w", err)
	}
	sockPath := storerpc.SocketPath(cfg)
	srvDone := make(chan error, 1)
	go func() {
		srvDone <- srv.Serve(ctx, ln)
	}()
	defer func() {
		ln.Close()
		os.Remove(sockPath)
		<-srvDone
	}()

	d := daemon.Daemon{
		Config: cfg,
		Store:  store,
		Log:    slog.Default(),
	}
	runErr := d.Run(ctx)

	// On graceful shutdown, optimize before the caller's deferred store.Close()
	// runs so that any records added via RPC since the last pass are FTS-durable.
	if optErr := store.Optimize(context.Background()); optErr != nil {
		slog.Warn("daemon: optimize on shutdown failed", "error", optErr)
	}

	if errors.Is(runErr, context.Canceled) {
		return nil
	}
	return runErr
}

func runScanOnce(ctx context.Context, cfg config.Config, store memory.API) error {
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
	{Name: "uninstall --yes", Summary: "remove managed local memory resources"},
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
