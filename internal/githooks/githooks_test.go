package githooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomnagengast/agent-memoryd/internal/config"
)

func TestWriteManagedHooksCreatesExecutableHooks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hooks")
	binary := filepath.Join(t.TempDir(), "agent-memoryd")

	if err := WriteManagedHooks(dir, binary); err != nil {
		t.Fatalf("write managed hooks: %v", err)
	}
	for _, name := range hookNames {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("%s is not executable: %v", name, info.Mode())
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(body)
		for _, want := range []string{
			"git rev-parse --git-common-dir",
			"enqueue-git --repo",
			"installed_bin='" + binary + "'",
			"hook_name='" + name + "'",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q:\n%s", name, want, text)
			}
		}
	}
}

func TestInstallManagedSetsUnsetGlobalHooksPath(t *testing.T) {
	requireGit(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), ".gitconfig"))
	cfg := config.Default()
	cfg.Root = t.TempDir()

	status, err := InstallManaged(cfg, "/tmp/agent-memoryd")
	if err != nil {
		t.Fatalf("install managed: %v", err)
	}
	if !status.Configured || status.HooksPath != config.ManagedGitHooksPath(cfg.Root) {
		t.Fatalf("status = %#v, want configured managed path", status)
	}
	got := gitConfig(t, "--global", "--get", "core.hooksPath")
	if strings.TrimSpace(got) != config.ManagedGitHooksPath(cfg.Root) {
		t.Fatalf("core.hooksPath = %q, want %q", strings.TrimSpace(got), config.ManagedGitHooksPath(cfg.Root))
	}
}

func TestInstallManagedDoesNotOverrideExistingGlobalHooksPath(t *testing.T) {
	requireGit(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), ".gitconfig"))
	existing := filepath.Join(t.TempDir(), "existing-hooks")
	gitConfig(t, "--global", "core.hooksPath", existing)
	cfg := config.Default()
	cfg.Root = t.TempDir()

	status, err := InstallManaged(cfg, "/tmp/agent-memoryd")
	if err != nil {
		t.Fatalf("install managed: %v", err)
	}
	if status.Configured || status.HooksPath != existing || status.Skipped == "" {
		t.Fatalf("status = %#v, want skipped existing hooks path", status)
	}
	got := gitConfig(t, "--global", "--get", "core.hooksPath")
	if strings.TrimSpace(got) != existing {
		t.Fatalf("core.hooksPath = %q, want %q", strings.TrimSpace(got), existing)
	}
}

func TestUninstallManagedUnsetsOnlyManagedGlobalHooksPath(t *testing.T) {
	requireGit(t)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), ".gitconfig"))
	cfg := config.Default()
	cfg.Root = t.TempDir()
	gitConfig(t, "--global", "core.hooksPath", config.ManagedGitHooksPath(cfg.Root))

	if err := UninstallManaged(cfg); err != nil {
		t.Fatalf("uninstall managed: %v", err)
	}
	if got := gitConfigAllowFailure(t, "--global", "--get", "core.hooksPath"); strings.TrimSpace(got) != "" {
		t.Fatalf("core.hooksPath = %q, want unset", strings.TrimSpace(got))
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found: %v", err)
	}
}

func gitConfig(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"config"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git config %v: %v: %s", args, err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func gitConfigAllowFailure(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"config"}, args...)...)
	out, _ := cmd.CombinedOutput()
	return string(out)
}
