package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// The Android host binds a fixed port (8787). Two things on a phone can hold
// it: a previous zen-gate that was killed without a clean Stop (START_STICKY
// recreates the service while the old socket is still in TIME_WAIT / held by
// the dying process), or the embedded Qoder hub on 8095. What the phone
// reports is only "网关启动失败", so the failure has to be diagnosable: it must
// name the port and come back in bounded time, never hang the foreground
// service.
func TestBootOnBusyPortFailsFastWithNamedError(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	busy := blocker.Addr().(*net.TCPAddr).Port

	done := make(chan error, 1)
	go func() {
		c, err := Start(context.Background(), Options{
			Home:           t.TempDir(),
			Port:           busy,
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
		if err == nil {
			t.Fatal("boot on a busy port reported success; the phone would show a running gateway that answers nothing")
		}
		if !contains(err.Error(), fmt.Sprint(busy)) {
			t.Fatalf("error does not name the port (%d): %v", busy, err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("boot on a busy port hung; the foreground service would never return")
	}
}

// A stopped gateway must release its listener. Without this, a STOP_START
// cycle on the phone (user taps 停止 then 启动, or START_STICKY recreates the
// service) hits "address already in use" and the second start looks like a
// random failure.
func TestStopReleasesListener(t *testing.T) {
	c, err := Start(context.Background(), Options{
		Home:           t.TempDir(),
		Port:           0,
		NoTray:         true,
		DisableDesktop: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	port := c.BaseURL()
	c.Stop()

	// The port must be bindable again right away.
	_, rest := cutHostPort(port)
	if rest == 0 {
		t.Skip("could not parse the boot port")
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", rest))
	if err != nil {
		t.Fatalf("listener not released after Stop: %v", err)
	}
	_ = ln.Close()
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func cutHostPort(base string) (string, int) {
	// "http://127.0.0.1:8787/v1"
	var p int
	if _, err := fmt.Sscanf(base, "http://127.0.0.1:%d/v1", &p); err != nil {
		return base, 0
	}
	return base, p
}

// The page probes /health before it draws anything; a gateway that is
// listening but not answering would look identical to a dead one.
func TestHealthAnswersAfterBoot(t *testing.T) {
	c, err := Start(context.Background(), Options{
		Home:           t.TempDir(),
		Port:           0,
		NoTray:         true,
		DisableDesktop: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	base := c.BaseURL()
	url := base[:len(base)-len("/v1")] + "/health"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health status %d", resp.StatusCode)
	}
}
