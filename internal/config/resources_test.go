package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitPersistsManagedResources(t *testing.T) {
	t.Setenv("AGENT_MEMORYD_HOME", filepath.Join(t.TempDir(), "agent-memoryd"))

	cfg, manifest, err := Init("")
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	want := []string{
		cfg.Root,
		ConfigPath(cfg.Root),
		ManifestPath(cfg.Root),
		cfg.SpoolDir,
		filepath.Join(cfg.Root, "logs"),
	}
	for _, path := range want {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected resource %s to exist: %v", path, err)
		}
	}
	if len(manifest.Resources) == 0 {
		t.Fatal("expected manifest resources")
	}
	foundGitHooks := false
	for _, resource := range manifest.Resources {
		if resource.Name == "global git hooks" && resource.Path == ManagedGitHooksPath(cfg.Root) {
			foundGitHooks = true
		}
	}
	if !foundGitHooks {
		t.Fatalf("manifest resources missing managed git hooks: %#v", manifest.Resources)
	}
	if _, err := os.Stat(cfg.StorePath); !os.IsNotExist(err) {
		t.Fatalf("legacy store path stat err = %v, want not exist", err)
	}
	for _, resource := range manifest.Resources {
		if resource.Name == "memory source store" {
			t.Fatalf("manifest includes deprecated JSONL store resource: %#v", manifest.Resources)
		}
	}
}

func TestLoadManifestFiltersDeprecatedMemorySourceStore(t *testing.T) {
	root := t.TempDir()
	manifest := Manifest{
		Version: manifestVersion,
		Resources: []Resource{
			{Name: "data root", Type: "directory", Path: root, Managed: true},
			{Name: "memory source store", Type: "data-file", Path: filepath.Join(root, "memories.jsonl"), Managed: true},
		},
	}
	if err := WriteManifest(root, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	loaded, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(loaded.Resources) != 1 {
		t.Fatalf("resource count = %d, want 1: %#v", len(loaded.Resources), loaded.Resources)
	}
	if loaded.Resources[0].Name != "data root" {
		t.Fatalf("remaining resource = %#v, want data root", loaded.Resources[0])
	}
}

func TestUninstallRemovesManagedRoot(t *testing.T) {
	t.Setenv("AGENT_MEMORYD_HOME", filepath.Join(t.TempDir(), "agent-memoryd"))

	cfg, manifest, err := Init("")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := Uninstall(cfg, manifest); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(cfg.Root); !os.IsNotExist(err) {
		t.Fatalf("root after uninstall stat err = %v, want not exist", err)
	}
}

func TestInitWritesSummarizerConfig(t *testing.T) {
	t.Setenv("AGENT_MEMORYD_HOME", filepath.Join(t.TempDir(), "agent-memoryd"))

	cfg, _, err := Init("")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	data, err := os.ReadFile(ConfigPath(cfg.Root))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var disk struct {
		SummarizerCommand  []string `json:"summarizer_command"`
		SummarizerTimeout  string   `json:"summarizer_timeout"`
		MemoryContextLimit int      `json:"memory_context_limit"`
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if len(disk.SummarizerCommand) == 0 || disk.SummarizerCommand[0] != "codex" {
		t.Fatalf("summarizer_command = %#v, want codex command", disk.SummarizerCommand)
	}
	if disk.SummarizerTimeout != "5m0s" {
		t.Fatalf("summarizer_timeout = %q, want 5m0s", disk.SummarizerTimeout)
	}
	if disk.MemoryContextLimit != 12 {
		t.Fatalf("memory_context_limit = %d, want 12", disk.MemoryContextLimit)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(loaded.SummarizerCommand) == 0 || loaded.SummarizerCommand[0] != "codex" {
		t.Fatalf("loaded summarizer command = %#v, want codex command", loaded.SummarizerCommand)
	}
	if loaded.MemoryContextLimit != 12 {
		t.Fatalf("loaded memory context limit = %d, want 12", loaded.MemoryContextLimit)
	}
}

func TestConfigMarshalJSONUsesDurationStrings(t *testing.T) {
	cfg := Default()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		`"poll_interval":"10s"`,
		`"idle_after":"2m0s"`,
		`"summarizer_timeout":"5m0s"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("marshaled config missing %s: %s", want, text)
		}
	}
}
