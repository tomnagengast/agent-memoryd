package memory

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

type Record struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Project   string    `json:"project,omitempty"`
	Source    string    `json:"source,omitempty"`
	Summary   string    `json:"summary"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AddRequest struct {
	ID      string
	Kind    string
	Project string
	Source  string
	Summary string
	Body    string
	Now     time.Time
}

func NewRecord(req AddRequest) (Record, error) {
	body := strings.TrimSpace(req.Body)
	if body == "" {
		return Record{}, ErrEmptyBody
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		var err error
		id, err = newID()
		if err != nil {
			return Record{}, err
		}
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "fact"
	}
	summary := strings.TrimSpace(req.Summary)
	if summary == "" {
		summary = Summarize(body, 180)
	}
	return Record{
		ID:        id,
		Kind:      kind,
		Project:   strings.TrimSpace(req.Project),
		Source:    strings.TrimSpace(req.Source),
		Summary:   summary,
		Body:      body,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (r Record) Updated(req AddRequest) (Record, error) {
	body := strings.TrimSpace(req.Body)
	if body == "" {
		return Record{}, ErrEmptyBody
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = r.Kind
	}
	summary := strings.TrimSpace(req.Summary)
	if summary == "" {
		summary = Summarize(body, 180)
	}
	r.Kind = kind
	r.Project = strings.TrimSpace(req.Project)
	r.Source = strings.TrimSpace(req.Source)
	r.Summary = summary
	r.Body = body
	r.UpdatedAt = now
	return r, nil
}

func Summarize(body string, max int) string {
	text := strings.Join(strings.Fields(body), " ")
	if len(text) <= max {
		return text
	}
	if max <= 1 {
		return text[:max]
	}
	return strings.TrimSpace(text[:max-1]) + "..."
}

func newID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
