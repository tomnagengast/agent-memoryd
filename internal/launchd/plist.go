package launchd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"text/template"
)

type Config struct {
	Label     string
	Binary    string
	Root      string
	LogDir    string
	PlistPath string
	Path      string
}

type Status struct {
	Supported bool   `json:"supported"`
	Installed bool   `json:"installed"`
	Started   bool   `json:"started"`
	Label     string `json:"label"`
	PlistPath string `json:"plist_path"`
	Skipped   string `json:"skipped,omitempty"`
}

const DefaultLabel = "dev.memoryd"

func Render(cfg Config) (string, error) {
	if cfg.Label == "" {
		cfg.Label = DefaultLabel
	}
	if cfg.Path == "" {
		cfg.Path = DefaultPath()
	}
	tpl := template.Must(template.New("plist").Parse(plistTemplate))
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, cfg); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func InstallAndStart(cfg Config) (Status, error) {
	if cfg.Label == "" {
		cfg.Label = DefaultLabel
	}
	if cfg.Path == "" {
		cfg.Path = DefaultPath()
	}
	status := CurrentStatus(cfg)
	if !status.Supported {
		status.Skipped = "launchd is only available on macOS"
		return status, nil
	}
	if cfg.PlistPath == "" {
		return status, fmt.Errorf("launchd plist path is empty")
	}
	text, err := Render(cfg)
	if err != nil {
		return status, err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.PlistPath), 0o755); err != nil {
		return status, fmt.Errorf("create launchd plist dir: %w", err)
	}
	if err := os.WriteFile(cfg.PlistPath, []byte(text), 0o644); err != nil {
		return status, fmt.Errorf("write launchd plist: %w", err)
	}
	status.Installed = true
	_ = runLaunchctl("bootout", launchdDomain(), cfg.PlistPath)
	if err := runLaunchctl("bootstrap", launchdDomain(), cfg.PlistPath); err != nil {
		return status, fmt.Errorf("bootstrap launchd service: %w", err)
	}
	if err := runLaunchctl("kickstart", "-k", launchdDomain()+"/"+cfg.Label); err != nil {
		return status, fmt.Errorf("kickstart launchd service: %w", err)
	}
	status.Started = true
	return status, nil
}

func CurrentStatus(cfg Config) Status {
	if cfg.Label == "" {
		cfg.Label = DefaultLabel
	}
	status := Status{
		Supported: runtime.GOOS == "darwin",
		Label:     cfg.Label,
		PlistPath: cfg.PlistPath,
	}
	if !status.Supported {
		status.Skipped = "launchd is only available on macOS"
		return status
	}
	if cfg.PlistPath != "" {
		_, err := os.Stat(cfg.PlistPath)
		status.Installed = err == nil
	}
	status.Started = daemonProcessRunning()
	return status
}

func BootoutAndRemove(path string) error {
	_ = runLaunchctl("bootout", launchdDomain(), path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func DefaultPath() string {
	if path := os.Getenv("PATH"); path != "" {
		return path
	}
	return "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
}

func launchdDomain() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func runLaunchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(bytes.TrimSpace(out))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func daemonProcessRunning() bool {
	return exec.Command("pgrep", "-f", "memoryd daemon").Run() == nil
}

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{{ .Label }}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{ .Binary }}</string>
    <string>daemon</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>EnvironmentVariables</key>
  <dict>
    <key>MEMORYD_HOME</key>
    <string>{{ .Root }}</string>
    <key>PATH</key>
    <string>{{ .Path }}</string>
  </dict>
  <key>StandardOutPath</key>
  <string>{{ .LogDir }}/memoryd.out.log</string>
  <key>StandardErrorPath</key>
  <string>{{ .LogDir }}/memoryd.err.log</string>
</dict>
</plist>
`
