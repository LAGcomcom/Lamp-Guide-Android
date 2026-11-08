package core

import (
	"context"
	"testing"
	"time"

)

// This reproduces the exact option set the Android host boots with
// (mobile.Mobile.Start): a fresh private data dir, port 8787, no tray,
// DisableDesktop, and a writable (NOT read-only) agent registry. It runs on the
// desktop so the same Go path can be exercised without a phone: if this hangs
// or errors, the phone's foreground service hangs or reports 启动失败, and the
// WebView never gets a gateway — which is what "DSH Go 没有回应" looks like from
// the outside.
func TestAndroidLikeBoot(t *testing.T) {
	home := t.TempDir()
	done := make(chan error, 1)
	go func() {
		c, err := Start(context.Background(), Options{
			Home:           home,
			Port:           8787,
			NoTray:         true,
			DisableDesktop: true,
		})
		if err == nil {
			defer c.Stop()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("android-like boot failed: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("android-like boot did not return within 30s (the phone's service would hang)")
	}
}

// The read-only decision must not swallow the Android switch. Android boots
// with DisableDesktop and still needs its one adapter to mint the peer key; only
// a preview host opts into read-only. Regressing this made the switch a no-op
// while the page claimed it was on.
func TestAgentsReadOnlyIsNotDisableDesktop(t *testing.T) {
	if agentsReadOnly(Options{DisableDesktop: true}) {
		t.Fatal("DisableDesktop must not imply read-only: it would disable the Android peer switch")
	}
	if agentsReadOnly(Options{DisableDesktop: true, ReadOnlyAgents: true}) != true {
		t.Fatal("ReadOnlyAgents must still be honoured")
	}
	if agentsReadOnly(Options{}) {
		t.Fatal("a plain desktop host writes agent configs by design")
	}
}
