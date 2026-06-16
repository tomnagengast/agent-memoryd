package launchd

import (
	"bytes"
	"text/template"
)

type Config struct {
	Label  string
	Binary string
	Root   string
	LogDir string
}

func Render(cfg Config) (string, error) {
	if cfg.Label == "" {
		cfg.Label = "dev.agent-memoryd"
	}
	tpl := template.Must(template.New("plist").Parse(plistTemplate))
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, cfg); err != nil {
		return "", err
	}
	return buf.String(), nil
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
    <key>AGENT_MEMORYD_HOME</key>
    <string>{{ .Root }}</string>
  </dict>
  <key>StandardOutPath</key>
  <string>{{ .LogDir }}/agent-memoryd.out.log</string>
  <key>StandardErrorPath</key>
  <string>{{ .LogDir }}/agent-memoryd.err.log</string>
</dict>
</plist>
`
