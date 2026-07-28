package job

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return OpenStore(filepath.Join(t.TempDir(), "nested", "jobs.json"))
}

func TestStorePutListDelete(t *testing.T) {
	s := testStore(t)
	base := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		rec := Record{
			ID:      NewID(base.Add(time.Duration(i) * time.Second)),
			Created: base.Add(time.Duration(i) * time.Second),
			Status:  StatusCompleted,
			Shell:   "app",
			Source:  fmt.Sprintf("export%d.csv", i),
			Request: json.RawMessage(`{"sourcePath":"x"}`),
		}
		if err := s.Put(rec); err != nil {
			t.Fatal(err)
		}
	}

	recs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || recs[0].Source != "export2.csv" {
		t.Fatalf("list wrong (want newest first): %+v", recs)
	}
	var req map[string]string
	if err := json.Unmarshal(recs[0].Request, &req); err != nil || req["sourcePath"] != "x" {
		t.Fatalf("request not preserved: %s (%v)", recs[0].Request, err)
	}

	if err := s.Delete(recs[1].ID); err != nil {
		t.Fatal(err)
	}
	recs, _ = s.List()
	if len(recs) != 2 {
		t.Fatalf("delete failed: %d records", len(recs))
	}
}

func TestStoreUpsertAndInterrupted(t *testing.T) {
	s := testStore(t)
	rec := Record{ID: "job-1", Created: time.Now(), Status: StatusRunning, Shell: "app", Source: "a.csv"}
	if err := s.Put(rec); err != nil {
		t.Fatal(err)
	}

	// Same ID updates in place.
	rec.Done = 10
	if err := s.Put(rec); err != nil {
		t.Fatal(err)
	}
	recs, _ := s.List()
	if len(recs) != 1 || recs[0].Done != 10 {
		t.Fatalf("upsert failed: %+v", recs)
	}

	// A record still running at next startup means the process died.
	n, err := s.MarkInterrupted()
	if err != nil || n != 1 {
		t.Fatalf("MarkInterrupted = %d, %v; want 1, nil", n, err)
	}
	recs, _ = s.List()
	if recs[0].Status != StatusInterrupted {
		t.Fatalf("status = %q, want interrupted", recs[0].Status)
	}
}

func TestStorePrunesToMax(t *testing.T) {
	s := testStore(t)
	base := time.Now()
	for i := 0; i < maxRecords+7; i++ {
		rec := Record{ID: fmt.Sprintf("job-%03d", i), Created: base.Add(time.Duration(i) * time.Second), Status: StatusCompleted}
		if err := s.Put(rec); err != nil {
			t.Fatal(err)
		}
	}
	recs, _ := s.List()
	if len(recs) != maxRecords {
		t.Fatalf("got %d records, want pruned to %d", len(recs), maxRecords)
	}
	if recs[0].ID != fmt.Sprintf("job-%03d", maxRecords+6) {
		t.Fatalf("pruned the wrong end: newest is %s", recs[0].ID)
	}
}

func TestStoreCorruptFileTreatedAsEmpty(t *testing.T) {
	s := testStore(t)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := s.List()
	if err != nil || len(recs) != 0 {
		t.Fatalf("corrupt history must read as empty, got %v, %v", recs, err)
	}
	if err := s.Put(Record{ID: "job-1", Created: time.Now(), Status: StatusCompleted}); err != nil {
		t.Fatal(err)
	}
}
