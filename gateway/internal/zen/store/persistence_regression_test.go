package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"zenui/internal/zen/lane"
)

func reviewStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("ZEN_GATE_HOME", t.TempDir())
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestRegressionStatsSnapshotIsolation(t *testing.T) {
	s := reviewStore(t)
	rec := lane.CallRecord{At: time.Now().UnixMilli(), Model: "review-model", Agent: "review-agent", Output: 1, Ok: true}
	s.Record(rec)
	days, _ := s.SnapshotStats()
	d := days[time.UnixMilli(rec.At).Format("2006-01-02")]
	s.Record(rec)
	if d.Models[rec.Model] != 1 || d.ModelReqs[rec.Model] != 1 || d.Agents[rec.Agent] != 1 {
		t.Fatalf("snapshot changed after Record: models=%d modelReqs=%d agents=%d; want all 1", d.Models[rec.Model], d.ModelReqs[rec.Model], d.Agents[rec.Agent])
	}
}
func TestRegressionStatsFlushRetainsDirtyOnFailure(t *testing.T) {
	s := reviewStore(t)
	s.Record(lane.CallRecord{At: time.Now().UnixMilli(), Model: "review-model", Output: 1, Ok: true})
	if err := os.Mkdir(filepath.Join(s.Home, "stats.json.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.FlushStats(); err == nil {
		t.Fatal("expected write failure")
	}
	if !s.stats.dirty {
		t.Fatal("failed write cleared dirty flag; next flush will skip retry")
	}
}
func TestRegressionPerfFlushRetainsDirtyOnFailure(t *testing.T) {
	s := reviewStore(t)
	s.AddTTFTSample("review-model", 100)
	if err := os.Mkdir(filepath.Join(s.Home, "perf.json.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	s.FlushPerf()
	if !s.perfDirty {
		t.Fatal("failed write cleared perfDirty flag; next flush will skip retry")
	}
}
