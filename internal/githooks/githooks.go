package githooks

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tomnagengast/agent-memoryd/internal/config"
)

var hookNames = []string{"post-commit", "post-merge", "post-rewrite"}

type Status struct {
	Supported   bool   `json:"supported"`
	ManagedPath string `json:"managed_path"`
	HooksPath   string `json:"hooks_path,omitempty"`
	Configured  bool   `json:"configured"`
	Installed   bool   `json:"installed"`
	Skipped     string `json:"skipped,omitempty"`
	Error       string `json:"error,omitempty"`
}

func InstallManaged(cfg config.Config, binary string) (Status, error) {
	managedPath := config.ManagedGitHooksPath(cfg.Root)
	if err := WriteManagedHooks(managedPath, binary); err != nil {
		return Status{}, err
	}

	status := Status{
		Supported:   true,
		ManagedPath: managedPath,
		Installed:   hooksExist(managedPath),
	}
	current, err := globalHooksPath()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			status.Supported = false
			status.Skipped = "git not found"
			return status, nil
		}
		return Status{}, err
	}
	status.HooksPath = current
	if current != "" && !samePath(current, managedPath) {
		status.Skipped = "global core.hooksPath is already set"
		return status, nil
	}
	if err := setGlobalHooksPath(managedPath); err != nil {
		return Status{}, err
	}
	status.HooksPath = managedPath
	status.Configured = true
	status.Skipped = ""
	return status, nil
}

func CurrentStatus(cfg config.Config) Status {
	managedPath := config.ManagedGitHooksPath(cfg.Root)
	status := Status{
		Supported:   true,
		ManagedPath: managedPath,
		Installed:   hooksExist(managedPath),
	}
	current, err := globalHooksPath()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			status.Supported = false
			status.Skipped = "git not found"
			return status
		}
		status.Error = err.Error()
		return status
	}
	status.HooksPath = current
	status.Configured = samePath(current, managedPath)
	if current != "" && !status.Configured {
		status.Skipped = "global core.hooksPath is already set"
	}
	return status
}

func UninstallManaged(cfg config.Config) error {
	managedPath := config.ManagedGitHooksPath(cfg.Root)
	current, err := globalHooksPath()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil
		}
		return err
	}
	if !samePath(current, managedPath) {
		return nil
	}
	cmd := exec.Command("git", "config", "--global", "--unset", "core.hooksPath")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("unset global git core.hooksPath: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func WriteManagedHooks(dir, binary string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create managed git hooks dir: %w", err)
	}
	for _, name := range hookNames {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(hookScript(name, binary)), 0o755); err != nil {
			return fmt.Errorf("write managed git hook %s: %w", name, err)
		}
	}
	return nil
}

func hookScript(name, binary string) string {
	return fmt.Sprintf(`#!/bin/sh
hook_name=%s
installed_bin=%s

run_legacy_hook() {
  git_common_dir="$(git rev-parse --git-common-dir 2>/dev/null)" || return 0
  case "$git_common_dir" in
    /*) ;;
    *) git_common_dir="$(pwd)/$git_common_dir" ;;
  esac
  legacy_hook="$git_common_dir/hooks/$hook_name"
  if [ -x "$legacy_hook" ] && [ "$legacy_hook" != "$0" ]; then
    "$legacy_hook" "$@"
  fi
}

find_agent_memoryd() {
  if command -v agent-memoryd >/dev/null 2>&1; then
    command -v agent-memoryd
    return 0
  fi
  if [ -n "$installed_bin" ] && [ -x "$installed_bin" ]; then
    printf '%%s\n' "$installed_bin"
    return 0
  fi
  if [ -n "$HOME" ] && [ -x "$HOME/.local/bin/agent-memoryd" ]; then
    printf '%%s\n' "$HOME/.local/bin/agent-memoryd"
    return 0
  fi
  return 1
}

run_agent_memoryd() {
  agent_memoryd_bin="$(find_agent_memoryd)" || return 0
  repo="$(git rev-parse --show-toplevel 2>/dev/null)" || return 0
  sha="$(git rev-parse HEAD 2>/dev/null)" || return 0
  "$agent_memoryd_bin" enqueue-git --repo "$repo" --sha "$sha" >/dev/null 2>&1 || true
}

run_legacy_hook "$@"
legacy_status=$?
if [ "$legacy_status" -ne 0 ]; then
  exit "$legacy_status"
fi
run_agent_memoryd
exit 0
`, shellQuote(name), shellQuote(binary))
}

func hooksExist(dir string) bool {
	for _, name := range hookNames {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return false
		}
	}
	return true
}

func globalHooksPath() (string, error) {
	cmd := exec.Command("git", "config", "--global", "--get", "core.hooksPath")
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", nil
	}
	return "", err
}

func setGlobalHooksPath(path string) error {
	cmd := exec.Command("git", "config", "--global", "core.hooksPath", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("set global git core.hooksPath: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	return filepath.Clean(expandHome(a)) == filepath.Clean(expandHome(b))
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return os.ExpandEnv(path)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	var buf bytes.Buffer
	buf.WriteByte('\'')
	for _, r := range value {
		if r == '\'' {
			buf.WriteString("'\\''")
			continue
		}
		buf.WriteRune(r)
	}
	buf.WriteByte('\'')
	return buf.String()
}
