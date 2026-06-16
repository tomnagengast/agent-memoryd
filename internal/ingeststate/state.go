package ingeststate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const maxAttempts = 3

type State struct {
	Inputs  map[string]Input `json:"inputs"`
	changed bool
}

type Input struct {
	Fingerprint   string     `json:"fingerprint"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	ProcessedAt   *time.Time `json:"processed_at,omitempty"`
}

func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &State{Inputs: map[string]Input{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read ingest state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode ingest state: %w", err)
	}
	if state.Inputs == nil {
		state.Inputs = map[string]Input{}
	}
	return &state, nil
}

func (s *State) Save(path string) error {
	if s == nil || !s.changed {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create ingest state dir: %w", err)
	}
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write ingest state temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace ingest state: %w", err)
	}
	s.changed = false
	return nil
}

func (s *State) ShouldProcess(key, fingerprint string, now time.Time) bool {
	if s == nil {
		return true
	}
	input, ok := s.Inputs[key]
	if !ok || input.Fingerprint != fingerprint {
		return true
	}
	switch input.Status {
	case "processed", "quarantined":
		return false
	case "failed":
		return input.NextAttemptAt == nil || !now.Before(*input.NextAttemptAt)
	default:
		return true
	}
}

func (s *State) MarkProcessed(key, fingerprint string, now time.Time) {
	if s == nil {
		return
	}
	processedAt := now.UTC()
	s.Inputs[key] = Input{
		Fingerprint: fingerprint,
		Status:      "processed",
		ProcessedAt: &processedAt,
	}
	s.changed = true
}

func (s *State) MarkFailed(key, fingerprint string, err error, now time.Time) Input {
	if s == nil {
		return Input{}
	}
	attempts := 1
	if existing, ok := s.Inputs[key]; ok && existing.Fingerprint == fingerprint {
		attempts = existing.Attempts + 1
	}
	lastAttemptAt := now.UTC()
	input := Input{
		Fingerprint:   fingerprint,
		Status:        "failed",
		Attempts:      attempts,
		LastAttemptAt: &lastAttemptAt,
		LastError:     err.Error(),
	}
	if attempts >= maxAttempts {
		input.Status = "quarantined"
	} else {
		nextAttemptAt := now.Add(backoff(attempts)).UTC()
		input.NextAttemptAt = &nextAttemptAt
	}
	s.Inputs[key] = input
	s.changed = true
	return input
}

func backoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	default:
		return 15 * time.Minute
	}
}
