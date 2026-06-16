package app

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunTopLevelHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		t.Run(arg, func(t *testing.T) {
			out, err := captureStdout(func() error {
				return Run([]string{arg})
			})
			if err != nil {
				t.Fatalf("Run(%q) returned error: %v", arg, err)
			}
			for _, want := range []string{
				"Local memory daemon for coding agents.",
				"Usage:",
				"Available Commands:",
				"mcp",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("help output missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestRunUnknownCommandMentionsHelp(t *testing.T) {
	err := Run([]string{"nope"})
	if err == nil {
		t.Fatal("Run returned nil error for unknown command")
	}
	if !strings.Contains(err.Error(), "agent-memoryd --help") {
		t.Fatalf("unknown command error did not mention help: %v", err)
	}
}

func TestRunArgumentErrorMentionsHelp(t *testing.T) {
	err := Run([]string{"add"})
	if err == nil {
		t.Fatal("Run returned nil error for missing add body")
	}
	if !strings.Contains(err.Error(), "agent-memoryd --help") {
		t.Fatalf("argument error did not mention help: %v", err)
	}
}

func TestRunSubcommandHelp(t *testing.T) {
	out, err := captureStdout(func() error {
		return Run([]string{"add", "--help"})
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	for _, want := range []string{
		"Create or update a memory.",
		"Usage:",
		"agent-memoryd add [flags] <body>",
		"--summary",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("subcommand help missing %q:\n%s", want, out)
		}
	}
}

func captureStdout(fn func() error) (string, error) {
	original := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = write

	fnErr := fn()
	closeErr := write.Close()
	os.Stdout = original

	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, read)
	readErr := read.Close()

	switch {
	case fnErr != nil:
		return buf.String(), fnErr
	case closeErr != nil:
		return buf.String(), closeErr
	case copyErr != nil:
		return buf.String(), copyErr
	case readErr != nil:
		return buf.String(), readErr
	default:
		return buf.String(), nil
	}
}
