package checkin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testManager(t *testing.T, targets []Target, claim func(context.Context, Target, string) Outcome) (*Manager, *time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, BusinessZone)
	m := New(t.TempDir(), func() []Target { return targets }, claim)
	m.now = func() time.Time { return now }
	return m, &now
}

func TestDailyIdempotencySurvivesRestartAndVisitsEveryAccount(t *testing.T) {
	accounts := []Target{{Channel: "test", Key: AccountKey("test", "one"), Label: "private-name", Mode: "automatic"}, {Channel: "test", Key: AccountKey("test", "two"), Mode: "automatic"}}
	calls := 0
	claim := func(_ context.Context, target Target, day string) Outcome {
		calls++
		return Outcome{Status: "claimed", Message: "confirmed", CoveredDay: day}
	}
	m, now := testManager(t, accounts, claim)
	if err := m.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("visited %d accounts", calls)
	}
	*now = now.Add(12 * time.Hour)
	m2 := New(filepath.Dir(m.path), func() []Target { return accounts }, claim)
	m2.now = m.now
	if err := m2.Run(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("restart/manual run repeated a completed day")
	}
	b, _ := os.ReadFile(m.path)
	if strings.Contains(string(b), "private-name") || strings.Contains(string(b), `"one"`) {
		t.Fatal("private labels/identities persisted")
	}
	*now = now.Add(24 * time.Hour)
	_ = m2.Run(context.Background(), false)
	if calls != 4 {
		t.Fatal("new business day was not claimed")
	}
}

func TestRetriesBackoffAndNextSlot(t *testing.T) {
	calls := 0
	m, now := testManager(t, []Target{{Channel: "a", Key: "key", Mode: "automatic"}}, func(context.Context, Target, string) Outcome {
		calls++
		return Outcome{Status: "failed", Message: "network"}
	})
	for i := 0; i < 5; i++ {
		_ = m.Run(context.Background(), false)
	}
	if calls != 1 {
		t.Fatal("failed account ignored backoff")
	}
	for _, delay := range []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour} {
		*now = now.Add(delay)
		_ = m.Run(context.Background(), false)
	}
	*now = now.Add(5 * time.Hour)
	_ = m.Run(context.Background(), false)
	if calls != 4 {
		t.Fatalf("expected capped 4 attempts, got %d", calls)
	}
	*now = time.Date(2026, 11, 6, 21, 0, 0, 0, BusinessZone)
	_ = m.Run(context.Background(), false)
	if calls != 5 {
		t.Fatal("next slot did not retry")
	}
	_ = m.Run(context.Background(), true)
	if calls != 6 {
		t.Fatal("explicit retry should bypass timer")
	}
}

func TestSwitchesManualAndUnsupportedNeverSend(t *testing.T) {
	accounts := []Target{{Channel: "off", Key: "1", Mode: "automatic"}, {Channel: "manual", Key: "2", Mode: "manual"}, {Channel: "none", Key: "3", Mode: "unsupported"}, {Channel: "on", Key: "4", Mode: "automatic"}}
	calls := 0
	m, _ := testManager(t, accounts, func(_ context.Context, target Target, day string) Outcome {
		calls++
		if target.Channel != "on" {
			t.Fatal("excluded channel called")
		}
		return Outcome{Status: "already", CoveredDay: day}
	})
	if err := m.Update(Settings{Enabled: false, Times: []string{"09:00"}, Disabled: map[string]bool{"off": true}}); err != nil {
		t.Fatal(err)
	}
	_ = m.Run(context.Background(), false)
	if calls != 0 {
		t.Fatal("master disabled")
	}
	_ = m.Run(context.Background(), true)
	if calls != 1 {
		t.Fatal("manual run should respect per-channel exclusion")
	}
}

func TestOldCampaignDoesNotCoverToday(t *testing.T) {
	calls := 0
	m, now := testManager(t, []Target{{Channel: "a", Key: "key", Mode: "automatic"}}, func(context.Context, Target, string) Outcome {
		calls++
		return Outcome{Status: "already", CoveredDay: "2026-11-05"}
	})
	_ = m.Run(context.Background(), false)
	if m.Snapshot().Accounts[0].Result.CoveredDay != "" {
		t.Fatal("stale reward advanced watermark")
	}
	*now = now.Add(5 * time.Minute)
	_ = m.Run(context.Background(), false)
	if calls != 2 {
		t.Fatal("stale reward suppressed today's retry")
	}
}

func TestUTC8CatchupAndNoEarlyRun(t *testing.T) {
	calls := 0
	m, now := testManager(t, []Target{{Channel: "a", Key: "key", Mode: "automatic"}}, func(_ context.Context, _ Target, day string) Outcome {
		calls++
		if day != "2026-11-07" {
			t.Fatal(day)
		}
		return Outcome{Status: "claimed", CoveredDay: day}
	})
	*now = time.Date(2026, 11, 7, 0, 59, 0, 0, time.UTC) // 08:59 China
	_ = m.Run(context.Background(), false)
	if calls != 0 {
		t.Fatal("ran before configured slot")
	}
	*now = now.Add(2 * time.Hour)
	_ = m.Run(context.Background(), false)
	if calls != 1 {
		t.Fatal("missed startup catchup")
	}
}

func TestConcurrentRunAndCancellation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	m, _ := testManager(t, []Target{{Channel: "a", Key: "key", Mode: "automatic"}}, func(ctx context.Context, _ Target, _ string) Outcome {
		close(entered)
		<-ctx.Done()
		close(release)
		return Outcome{Status: "failed"}
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := m.Trigger(ctx); err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := m.Trigger(ctx); !errors.Is(err, ErrBusy) {
		t.Fatal("overlapping runs accepted", err)
	}
	cancel()
	<-release
	deadline := time.Now().Add(time.Second)
	for m.Snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if m.Snapshot().Running {
		t.Fatal("cancelled run did not finish")
	}
}

func TestCorruptStateAndWriteFailureStopBeforeNetwork(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{true: "corrupt", false: "write-failure"}[corrupt], func(t *testing.T) {
			home := t.TempDir()
			calls := 0
			if corrupt {
				_ = os.WriteFile(filepath.Join(home, "checkin.json"), []byte("broken"), 0600)
			}
			m := New(home, func() []Target { return []Target{{Channel: "a", Key: "k", Mode: "automatic"}} }, func(context.Context, Target, string) Outcome { calls++; return Outcome{} })
			if !corrupt {
				m.path = filepath.Join(home, "missing-dir", "checkin.json")
			}
			if err := m.Run(context.Background(), true); err == nil {
				t.Fatal("expected storage error")
			}
			if calls != 0 {
				t.Fatal("claim sent without safe state")
			}
		})
	}
}

func TestSettingsValidationAndSnapshotIsolation(t *testing.T) {
	m, _ := testManager(t, nil, nil)
	for _, times := range [][]string{nil, {"9:00"}, {"25:00"}, {"09:00", "09:00"}} {
		if m.Update(Settings{Times: times}) == nil {
			t.Fatalf("accepted %v", times)
		}
	}
	view := m.Snapshot()
	view.Settings.Times[0] = "12:00"
	view.Settings.Disabled["a"] = true
	if m.Snapshot().Settings.Times[0] != "09:00" || m.Snapshot().Settings.Disabled["a"] {
		t.Fatal("snapshot shares mutable state")
	}
}
