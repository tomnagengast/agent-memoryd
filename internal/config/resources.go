package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const manifestVersion = 1

type Resource struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Path    string `json:"path"`
	Managed bool   `json:"managed"`
	Exists  bool   `json:"exists"`
}

type Manifest struct {
	Version   int        `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	Resources []Resource `json:"resources"`
}

func Init(path string) (Config, Manifest, error) {
	cfg := Default()
	if path == "" {
		path = ConfigPath(cfg.Root)
	}
	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
		return Config{}, Manifest{}, fmt.Errorf("create root dir: %w", err)
	}
	for _, dir := range []string{cfg.SpoolDir, filepath.Join(cfg.Root, "logs")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Config{}, Manifest{}, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	if _, err := os.Stat(cfg.StorePath); os.IsNotExist(err) {
		file, err := os.OpenFile(cfg.StorePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return Config{}, Manifest{}, fmt.Errorf("create memory store: %w", err)
		}
		if err := file.Close(); err != nil {
			return Config{}, Manifest{}, fmt.Errorf("close memory store: %w", err)
		}
	} else if err != nil {
		return Config{}, Manifest{}, fmt.Errorf("stat memory store: %w", err)
	}
	if err := writeDefaultTo(path, cfg); err != nil {
		return Config{}, Manifest{}, err
	}
	manifest := Manifest{
		Version:   manifestVersion,
		CreatedAt: time.Now().UTC(),
		Resources: plannedResources(cfg, path),
	}
	if err := WriteManifest(cfg.Root, manifest); err != nil {
		return Config{}, Manifest{}, err
	}
	return cfg, withExists(manifest), nil
}

func LoadManifest(root string) (Manifest, error) {
	path := ManifestPath(root)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := withRoot(Default(), root)
		return withExists(Manifest{
			Version:   manifestVersion,
			Resources: plannedResources(cfg, ConfigPath(root)),
		}), nil
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("read resource manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode resource manifest: %w", err)
	}
	return withExists(manifest), nil
}

func withRoot(cfg Config, root string) Config {
	cfg.Root = root
	cfg.StorePath = filepath.Join(root, "memories.jsonl")
	cfg.ZvecPath = filepath.Join(root, "zvec")
	cfg.SpoolDir = filepath.Join(root, "spool")
	return cfg
}

func WriteManifest(root string, manifest Manifest) error {
	path := ManifestPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func ManifestPath(root string) string {
	return filepath.Join(root, "resources.json")
}

func Uninstall(cfg Config, manifest Manifest) error {
	for _, resource := range manifest.Resources {
		if resource.Type == "launchd-plist" && resource.Managed && exists(resource.Path) {
			_ = bootoutLaunchAgent(resource.Path)
			if err := os.Remove(resource.Path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove launchd plist: %w", err)
			}
		}
	}
	if cfg.Root == "" || cfg.Root == "/" || cfg.Root == "." {
		return fmt.Errorf("refusing to remove unsafe root %q", cfg.Root)
	}
	if err := os.RemoveAll(cfg.Root); err != nil {
		return fmt.Errorf("remove data root: %w", err)
	}
	for _, resource := range manifest.Resources {
		if resource.Type == "config-file" && resource.Managed && !isWithin(resource.Path, cfg.Root) {
			if err := os.Remove(resource.Path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove config file: %w", err)
			}
		}
	}
	return nil
}

func plannedResources(cfg Config, configPath string) []Resource {
	return []Resource{
		{Name: "data root", Type: "directory", Path: cfg.Root, Managed: true},
		{Name: "config file", Type: "config-file", Path: configPath, Managed: true},
		{Name: "resource manifest", Type: "manifest-file", Path: ManifestPath(cfg.Root), Managed: true},
		{Name: "memory source store", Type: "data-file", Path: cfg.StorePath, Managed: true},
		{Name: "zvec index", Type: "index-directory", Path: cfg.ZvecPath, Managed: true},
		{Name: "git event spool", Type: "directory", Path: cfg.SpoolDir, Managed: true},
		{Name: "logs", Type: "directory", Path: filepath.Join(cfg.Root, "logs"), Managed: true},
		{Name: "launchd plist", Type: "launchd-plist", Path: launchdPlistPath(), Managed: true},
	}
}

func withExists(manifest Manifest) Manifest {
	for i := range manifest.Resources {
		manifest.Resources[i].Exists = exists(manifest.Resources[i].Path)
	}
	return manifest
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeDefaultTo(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	body := map[string]any{
		"root":                 cfg.Root,
		"store_path":           cfg.StorePath,
		"index_backend":        cfg.IndexBackend,
		"zvec_path":            cfg.ZvecPath,
		"spool_dir":            cfg.SpoolDir,
		"transcript_roots":     cfg.TranscriptRoots,
		"summarizer_command":   cfg.SummarizerCommand,
		"summarizer_timeout":   cfg.SummarizerTimeout.String(),
		"memory_context_limit": cfg.MemoryContextLimit,
		"poll_interval":        cfg.PollInterval.String(),
		"idle_after":           cfg.IdleAfter.String(),
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func launchdPlistPath() string {
	return filepath.Join(homeDir(), "Library", "LaunchAgents", "dev.agent-memoryd.plist")
}

func bootoutLaunchAgent(path string) error {
	uid := strconv.Itoa(os.Getuid())
	cmd := exec.Command("launchctl", "bootout", "gui/"+uid, path)
	return cmd.Run()
}

func isWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
