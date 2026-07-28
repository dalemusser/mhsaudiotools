package job

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Job record statuses. A record still "running" when a store is next opened
// belongs to a process that died mid-run; MarkInterrupted turns it into
// StatusInterrupted so a UI can offer to resume it.
const (
	StatusRunning     = "running"
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusCanceled    = "canceled"
	StatusInterrupted = "interrupted"
)

// maxRecords bounds the history; older records are pruned on write.
const maxRecords = 50

// Record is one generation run, remembered across restarts. The engine treats
// Request as opaque — it is the owning shell's replayable request (the app
// resumes a job by handing it straight back to Generate).
type Record struct {
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	Status  string    `json:"status"`
	Shell   string    `json:"shell"` // "app" or "cli"

	// Display fields for listings.
	Source    string `json:"source"`
	OutputDir string `json:"outputDir"`
	Layout    string `json:"layout,omitempty"`

	Request json.RawMessage `json:"request,omitempty"`

	Targets int    `json:"targets,omitempty"`
	Done    int    `json:"done,omitempty"`
	Written int    `json:"written,omitempty"`
	Failed  int    `json:"failed,omitempty"`
	Error   string `json:"error,omitempty"`
}

// NewID returns a unique-enough record ID; nanosecond resolution covers two
// runs started back to back.
func NewID(now time.Time) string {
	return "job-" + now.Format("20060102-150405.000000000")
}

// Store persists job records as one JSON file. Reads and writes are whole-file
// (load, modify, atomic replace) — the history is small and the tool is
// single-user, so last-writer-wins between the CLI and the app is acceptable.
type Store struct {
	path string
	mu   sync.Mutex
}

// DefaultStorePath is the shared history location for both shells.
func DefaultStorePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mhsaudio", "jobs.json"), nil
}

func OpenStore(path string) *Store { return &Store{path: path} }

// List returns records newest-first.
func (s *Store) List() ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Put inserts or replaces the record (matched by ID), stamps Updated, prunes
// old history, and writes atomically.
func (s *Store) Put(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, err := s.load()
	if err != nil {
		return err
	}
	rec.Updated = time.Now()
	replaced := false
	for i := range recs {
		if recs[i].ID == rec.ID {
			recs[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		recs = append(recs, rec)
	}
	return s.write(recs)
}

// Delete removes a record by ID; deleting a missing ID is a no-op.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, err := s.load()
	if err != nil {
		return err
	}
	kept := recs[:0]
	for _, r := range recs {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	return s.write(kept)
}

// MarkInterrupted flips any record still "running" to "interrupted" — called at
// startup, when a running record can only mean the previous process died.
func (s *Store) MarkInterrupted() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, err := s.load()
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range recs {
		if recs[i].Status == StatusRunning {
			recs[i].Status = StatusInterrupted
			recs[i].Updated = time.Now()
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.write(recs)
}

func (s *Store) load() ([]Record, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var recs []Record
	if err := json.Unmarshal(data, &recs); err != nil {
		// History is a convenience, never worth blocking a run over — treat a
		// corrupt file as empty; the next write replaces it.
		return nil, nil
	}
	return recs, nil
}

func (s *Store) write(recs []Record) error {
	sort.Slice(recs, func(i, j int) bool { return recs[i].Created.After(recs[j].Created) })
	if len(recs) > maxRecords {
		recs = recs[:maxRecords]
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "jobs-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Summary renders a one-line description for terminal listings.
func (r Record) Summary() string {
	src := filepath.Base(r.Source)
	when := r.Created.Format("2006-01-02 15:04")
	counts := fmt.Sprintf("%d/%d written", r.Written, r.Targets)
	if r.Failed > 0 {
		counts += fmt.Sprintf(", %d failed", r.Failed)
	}
	return fmt.Sprintf("%s  %-11s %-4s %s → %s  (%s)", when, r.Status, r.Shell, src, r.OutputDir, counts)
}
