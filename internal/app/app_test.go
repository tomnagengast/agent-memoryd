package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/summarizer"
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

func TestRunVersion(t *testing.T) {
	for _, arg := range []string{"-v", "--version"} {
		t.Run(arg, func(t *testing.T) {
			out, err := captureStdout(func() error {
				return Run([]string{arg})
			})
			if err != nil {
				t.Fatalf("Run(%q) returned error: %v", arg, err)
			}
			if !strings.Contains(out, "memoryd") {
				t.Fatalf("version output missing binary name:\n%s", out)
			}
		})
	}
}

func TestRunUnknownCommandMentionsHelp(t *testing.T) {
	err := Run([]string{"nope"})
	if err == nil {
		t.Fatal("Run returned nil error for unknown command")
	}
	if !strings.Contains(err.Error(), "memoryd --help") {
		t.Fatalf("unknown command error did not mention help: %v", err)
	}
}

func TestRunArgumentErrorMentionsHelp(t *testing.T) {
	err := Run([]string{"add"})
	if err == nil {
		t.Fatal("Run returned nil error for missing add body")
	}
	if !strings.Contains(err.Error(), "memoryd --help") {
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
		"memoryd add [flags] <body>",
		"--summary",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("subcommand help missing %q:\n%s", want, out)
		}
	}
}

type fakeReflectSummarizer struct {
	req summarizer.Request
}

func (f *fakeReflectSummarizer) Summarize(_ context.Context, req summarizer.Request) (summarizer.Result, error) {
	f.req = req
	return summarizer.Result{Memories: []summarizer.GeneratedMemory{{
		Kind:    "preference",
		Summary: "Remember reflection preference",
		Body:    "User wants a reflect tool to persist memories from the current session.",
	}}}, nil
}

func TestReflectSessionTextStoresSummarizedMemory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := memory.NewStore(filepath.Join(t.TempDir(), "memories.jsonl"))
	fake := &fakeReflectSummarizer{}
	in := reflectInput{
		Project: "agent-memoryd",
		CWD:     "/tmp/agent-memoryd",
		Source:  "session:test",
		Session: "raw current session content that should only go to the summarizer",
	}

	records, err := reflectSessionText(ctx, store, fake, in, 5)
	if err != nil {
		t.Fatalf("reflect session text: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if fake.req.Producer != "reflect" || !strings.Contains(fake.req.SourceMaterial, in.Session) {
		t.Fatalf("summarizer request = %#v, want reflect request with session material", fake.req)
	}
	if records[0].Kind != "preference" {
		t.Fatalf("record kind = %q, want preference", records[0].Kind)
	}
	if !strings.Contains(records[0].Body, "More detail: Session: session:test") {
		t.Fatalf("record body missing detail reference: %q", records[0].Body)
	}
	if strings.Contains(records[0].Body, in.Session) {
		t.Fatalf("record body contains raw session material: %q", records[0].Body)
	}
}

func TestLatestTranscriptReturnsNewestJSONL(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.jsonl")
	newPath := filepath.Join(root, "new.jsonl")
	for _, path := range []string{oldPath, newPath} {
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
	}
	oldTime := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtime old transcript: %v", err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatalf("chtime new transcript: %v", err)
	}

	got, err := latestTranscript([]string{root})
	if err != nil {
		t.Fatalf("latest transcript: %v", err)
	}
	if got != newPath {
		t.Fatalf("latest transcript = %q, want %q", got, newPath)
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
