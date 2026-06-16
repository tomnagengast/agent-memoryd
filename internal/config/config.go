package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Root            string        `json:"root"`
	StorePath       string        `json:"store_path"`
	IndexBackend    string        `json:"index_backend"`
	ZvecPath        string        `json:"zvec_path"`
	SpoolDir        string        `json:"spool_dir"`
	TranscriptRoots []string      `json:"transcript_roots"`
	PollInterval    time.Duration `json:"poll_interval"`
	IdleAfter       time.Duration `json:"idle_after"`
}

func Default() Config {
	root := dataRoot()
	return Config{
		Root:         root,
		StorePath:    filepath.Join(root, "memories.jsonl"),
		IndexBackend: "lexical",
		ZvecPath:     filepath.Join(root, "zvec"),
		SpoolDir:     filepath.Join(root, "spool"),
		TranscriptRoots: []string{
			filepath.Join(homeDir(), ".claude", "projects"),
			filepath.Join(homeDir(), ".codex", "sessions"),
		},
		PollInterval: 10 * time.Second,
		IdleAfter:    2 * time.Minute,
	}
}

func Load() (Config, error) {
	cfg := Default()
	path := ConfigPath(cfg.Root)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var disk struct {
		Root            string   `json:"root"`
		StorePath       string   `json:"store_path"`
		IndexBackend    string   `json:"index_backend"`
		ZvecPath        string   `json:"zvec_path"`
		SpoolDir        string   `json:"spool_dir"`
		TranscriptRoots []string `json:"transcript_roots"`
		PollInterval    string   `json:"poll_interval"`
		IdleAfter       string   `json:"idle_after"`
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if disk.Root != "" {
		cfg.Root = expand(disk.Root)
	}
	if disk.StorePath != "" {
		cfg.StorePath = expand(disk.StorePath)
	}
	if disk.IndexBackend != "" {
		cfg.IndexBackend = disk.IndexBackend
	}
	if disk.ZvecPath != "" {
		cfg.ZvecPath = expand(disk.ZvecPath)
	}
	if disk.SpoolDir != "" {
		cfg.SpoolDir = expand(disk.SpoolDir)
	}
	if disk.TranscriptRoots != nil {
		cfg.TranscriptRoots = make([]string, 0, len(disk.TranscriptRoots))
		for _, root := range disk.TranscriptRoots {
			cfg.TranscriptRoots = append(cfg.TranscriptRoots, expand(root))
		}
	}
	if disk.PollInterval != "" {
		d, err := time.ParseDuration(disk.PollInterval)
		if err != nil {
			return Config{}, fmt.Errorf("parse poll_interval: %w", err)
		}
		cfg.PollInterval = d
	}
	if disk.IdleAfter != "" {
		d, err := time.ParseDuration(disk.IdleAfter)
		if err != nil {
			return Config{}, fmt.Errorf("parse idle_after: %w", err)
		}
		cfg.IdleAfter = d
	}
	return cfg, nil
}

func WriteDefault(path string) error {
	cfg := Default()
	if path == "" {
		path = ConfigPath(cfg.Root)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	body := map[string]any{
		"root":             cfg.Root,
		"store_path":       cfg.StorePath,
		"index_backend":    cfg.IndexBackend,
		"zvec_path":        cfg.ZvecPath,
		"spool_dir":        cfg.SpoolDir,
		"transcript_roots": cfg.TranscriptRoots,
		"poll_interval":    cfg.PollInterval.String(),
		"idle_after":       cfg.IdleAfter.String(),
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func ConfigPath(root string) string {
	return filepath.Join(root, "config.json")
}

func dataRoot() string {
	if root := os.Getenv("AGENT_MEMORYD_HOME"); root != "" {
		return expand(root)
	}
	return filepath.Join(homeDir(), ".local", "share", "agent-memoryd")
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

func expand(path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir(), path[2:])
	}
	return os.ExpandEnv(path)
}
