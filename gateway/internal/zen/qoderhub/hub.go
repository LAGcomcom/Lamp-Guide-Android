package qoderhub

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"zenui/internal/zen/qoderhub/httpapi"
	"zenui/internal/zen/qoderhub/remote"
	"zenui/internal/zen/qoderhub/service"
)

// The embedded QoderCN gateway: the vendored qodercn-gateway (MIT, by
// Tiancheng Lu) runs inside zen-gate's process on 127.0.0.1:8095 and serves
// the OpenAI/Anthropic surface the Qoder 账号渠道 provider points at. It
// harvests the QoderCN IDE's cached login, so "logged in" follows the IDE.
//
// Start is non-fatal by design: when the port is already taken (an external
// qodercn-gateway the user runs themselves, or a second zen-gate), the hub
// stays down and the status probe simply reports whatever answers on the port.

// HubHost/HubPort is the fixed loopback bind the managed provider points at.
const (
	HubHost = "127.0.0.1"
	HubPort = 8095
)

// BaseURL is what the managed provider's BaseURL is built from.
const BaseURL = "http://127.0.0.1:8095/v1"

// Hub is one embedded gateway instance.
type Hub struct {
	svc    *service.Service
	server *httpapi.Server
	addr   string
}

// Start brings the hub up. Warmup happens asynchronously — a slow or absent
// IDE login must never delay zen-gate's own startup.
//
// authFile is the credential file the hub reads; empty means "follow the Qoder
// IDE's own cache". zen-gate passes its own path so that a login performed in
// the dashboard (渠道面 的「登录」) is what the model calls use, instead of the
// IDE's possibly stale cache.
func Start(authFile string, sources ...func() []remote.Credential) (*Hub, error) {
	addr := net.JoinHostPort(HubHost, fmt.Sprintf("%d", HubPort))
	// Fail fast on a busy port so the caller can fall back to whatever
	// already answers there instead of half-starting.
	if ln, err := net.Listen("tcp", addr); err != nil {
		return nil, fmt.Errorf("port %d occupied: %w", HubPort, err)
	} else {
		_ = ln.Close()
	}

	cfg := service.Config{
		Host:           HubHost,
		Port:           HubPort,
		Model:          "kmodel",
		SessionMode:    "auto",
		RemoteAuthFile: authFile,
	}
	if len(sources) > 0 {
		cfg.RemoteCredentials = sources[0]
	}
	svc := service.New(cfg)
	h := &Hub{svc: svc, addr: addr}
	h.server = httpapi.NewServer(addr, svc)

	go func() {
		warmCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := svc.Warmup(warmCtx); err != nil {
			// Not fatal: the credential may arrive later (IDE login) and the
			// client re-reads the cache per request.
			_ = err
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for the listener to answer, not for a fixed clock. The bind is on
	// loopback and completes in a few milliseconds; the old 500ms timeout was
	// pure startup latency added to every launch (it sat inside core.Start, so
	// the window waited half a second before it could even be created).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			return nil, err
		default:
		}
		if c, err := net.DialTimeout("tcp", addr, 20*time.Millisecond); err == nil {
			_ = c.Close()
			return h, nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return h, nil
}

// Stop drains the hub.
func (h *Hub) Stop() {
	if h == nil || h.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = h.server.Shutdown(ctx)
	_ = h.svc.Close()
}
