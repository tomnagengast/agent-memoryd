package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

const CommandName = "memoryd"

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func String() string {
	version := strings.TrimSpace(Version)
	if version == "" {
		version = "dev"
	}
	commit := strings.TrimSpace(Commit)
	if commit == "" {
		commit = vcsRevision()
	}
	date := strings.TrimSpace(Date)
	if commit == "" && date == "" {
		return fmt.Sprintf("%s %s", CommandName, version)
	}
	if date == "" {
		return fmt.Sprintf("%s %s (%s)", CommandName, version, commit)
	}
	if commit == "" {
		return fmt.Sprintf("%s %s (%s)", CommandName, version, date)
	}
	return fmt.Sprintf("%s %s (%s, %s)", CommandName, version, commit, date)
}

func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var revision string
	var modified bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified && revision != "" {
		revision += "-dirty"
	}
	return revision
}
