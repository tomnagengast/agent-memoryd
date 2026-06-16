package ingeststate

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStateBackoffAndQuarantine(t *testing.T) {
	t.Parallel()
	state := &State{Inputs: map[string]Input{}}
	key := "transcript:/tmp/session.jsonl"
	fp := "fingerprint"
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

	first := state.MarkFailed(key, fp, errors.New("failed"), now)
	if first.Status != "failed" || first.Attempts != 1 || first.NextAttemptAt == nil {
		t.Fatalf("first failure = %#v, want failed with next attempt", first)
	}
	if state.ShouldProcess(key, fp, now.Add(30*time.Second)) {
		t.Fatal("should not process during backoff")
	}
	state.MarkFailed(key, fp, errors.New("failed"), now.Add(2*time.Minute))
	third := state.MarkFailed(key, fp, errors.New("failed"), now.Add(10*time.Minute))
	if third.Status != "quarantined" || third.Attempts != 3 {
		t.Fatalf("third failure = %#v, want quarantined", third)
	}
	if state.ShouldProcess(key, fp, now.Add(time.Hour)) {
		t.Fatal("should not process quarantined fingerprint")
	}
	if !state.ShouldProcess(key, "new-fingerprint", now.Add(time.Hour)) {
		t.Fatal("changed fingerprint should be eligible")
	}
}

func TestStateSaveLoad(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ingest-state.json")
	state := &State{Inputs: map[string]Input{}}
	state.MarkProcessed("git:event", "fingerprint", time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC))
	if err := state.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	input := loaded.Inputs["git:event"]
	if input.Status != "processed" || input.Fingerprint != "fingerprint" {
		t.Fatalf("loaded input = %#v, want processed fingerprint", input)
	}
}
