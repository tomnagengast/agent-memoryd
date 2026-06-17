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
	Root               string        `json:"root"`
	StorePath          string        `json:"store_path"`
	IndexBackend       string        `json:"index_backend"`
	ZvecPath           string        `json:"zvec_path"`
	SpoolDir           string        `json:"spool_dir"`
	TranscriptRoots    []string      `json:"transcript_roots"`
	SummarizerCommand  []string      `json:"summarizer_command"`
	SummarizerTimeout  time.Duration `json:"summarizer_timeout"`
	MemoryContextLimit int           `json:"memory_context_limit"`
	PollInterval       time.Duration `json:"poll_interval"`
	IdleAfter          time.Duration `json:"idle_after"`
}

func (c Config) MarshalJSON() ([]byte, error) {
	type diskConfig struct {
		Root               string   `json:"root"`
		StorePath          string   `json:"store_path"`
		IndexBackend       string   `json:"index_backend"`
		ZvecPath           string   `json:"zvec_path"`
		SpoolDir           string   `json:"spool_dir"`
		TranscriptRoots    []string `json:"transcript_roots"`
		SummarizerCommand  []string `json:"summarizer_command"`
		SummarizerTimeout  string   `json:"summarizer_timeout"`
		MemoryContextLimit int      `json:"memory_context_limit"`
		PollInterval       string   `json:"poll_interval"`
		IdleAfter          string   `json:"idle_after"`
	}
	return json.Marshal(diskConfig{
		Root:               c.Root,
		StorePath:          c.StorePath,
		IndexBackend:       c.IndexBackend,
		ZvecPath:           c.ZvecPath,
		SpoolDir:           c.SpoolDir,
		TranscriptRoots:    c.TranscriptRoots,
		SummarizerCommand:  c.SummarizerCommand,
		SummarizerTimeout:  c.SummarizerTimeout.String(),
		MemoryContextLimit: c.MemoryContextLimit,
		PollInterval:       c.PollInterval.String(),
		IdleAfter:          c.IdleAfter.String(),
	})
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
			filepath.Join(homeDir(), ".local", "share", "opencode"),
		},
		SummarizerCommand: []string{
			"codex",
			"exec",
			"--sandbox",
			"read-only",
			"--skip-git-repo-check",
			"--ephemeral",
			"-",
		},
		SummarizerTimeout:  5 * time.Minute,
		MemoryContextLimit: 12,
		PollInterval:       10 * time.Second,
		IdleAfter:          2 * time.Minute,
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
		Root               string   `json:"root"`
		StorePath          string   `json:"store_path"`
		IndexBackend       string   `json:"index_backend"`
		ZvecPath           string   `json:"zvec_path"`
		SpoolDir           string   `json:"spool_dir"`
		TranscriptRoots    []string `json:"transcript_roots"`
		SummarizerCommand  []string `json:"summarizer_command"`
		SummarizerTimeout  string   `json:"summarizer_timeout"`
		MemoryContextLimit int      `json:"memory_context_limit"`
		PollInterval       string   `json:"poll_interval"`
		IdleAfter          string   `json:"idle_after"`
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
	if disk.SummarizerCommand != nil {
		cfg.SummarizerCommand = append([]string(nil), disk.SummarizerCommand...)
	}
	if disk.SummarizerTimeout != "" {
		d, err := time.ParseDuration(disk.SummarizerTimeout)
		if err != nil {
			return Config{}, fmt.Errorf("parse summarizer_timeout: %w", err)
		}
		cfg.SummarizerTimeout = d
	}
	if disk.MemoryContextLimit > 0 {
		cfg.MemoryContextLimit = disk.MemoryContextLimit
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
	return writeDefaultTo(path, cfg)
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
