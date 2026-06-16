package config

import (
	"os"
	"path/filepath"
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
		cfg.StorePath,
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
