package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/config"
	"github.com/tomnagengast/agent-memoryd/internal/ingest"
	"github.com/tomnagengast/agent-memoryd/internal/ingeststate"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
	"github.com/tomnagengast/agent-memoryd/internal/spool"
	"github.com/tomnagengast/agent-memoryd/internal/summarizer"
)

type Daemon struct {
	Config config.Config
	Store  *memory.Store
	Log    *slog.Logger
}

func (d Daemon) Run(ctx context.Context) error {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Config.PollInterval <= 0 {
		d.Config.PollInterval = 10 * time.Second
	}
	if err := d.Once(ctx); err != nil {
		d.Log.Warn("initial daemon pass failed", "error", err)
	}
	ticker := time.NewTicker(d.Config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := d.Once(ctx); err != nil {
				d.Log.Warn("daemon pass failed", "error", err)
			}
		}
	}
}

func (d Daemon) Once(ctx context.Context) (err error) {
	statePath := config.IngestStatePath(d.Config.Root)
	state, err := ingeststate.Load(statePath)
	if err != nil {
		return err
	}
	defer func() {
		if saveErr := state.Save(statePath); err == nil && saveErr != nil {
			err = saveErr
		}
	}()

	agent := summarizer.CommandAgent{
		Command: d.Config.SummarizerCommand,
		Timeout: d.Config.SummarizerTimeout,
	}
	gitEvents, err := spool.ProcessGit(ctx, d.Config.SpoolDir, d.Store, agent, d.Config.MemoryContextLimit, state)
	if err != nil {
		return err
	}
	scanner := ingest.Scanner{
		Roots:              d.Config.TranscriptRoots,
		IdleAfter:          d.Config.IdleAfter,
		NotBefore:          d.transcriptNotBefore(),
		Summarizer:         agent,
		MemoryContextLimit: d.Config.MemoryContextLimit,
		State:              state,
	}
	sessions, err := scanner.Scan(ctx, d.Store)
	if err != nil {
		return err
	}
	if d.Log != nil && (gitEvents > 0 || sessions > 0) {
		d.Log.Info("processed memory inputs", "git_events", gitEvents, "sessions", sessions)
	}
	return nil
}

func (d Daemon) transcriptNotBefore() time.Time {
	manifest, err := config.LoadManifest(d.Config.Root)
	if err != nil {
		return time.Time{}
	}
	return manifest.CreatedAt
}
