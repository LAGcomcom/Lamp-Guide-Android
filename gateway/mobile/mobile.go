// Package mobile is the gomobile binding surface for the Android host.
//
// gomobile bind turns the exported functions below into a Java/Kotlin API
// inside an AAR. The Kotlin side (GatewayService) owns the foreground-service
// lifecycle; MainActivity owns the WebView. Neither of them talks to the
// gateway over HTTP: every admin call goes through Admin, which runs the real
// router in-process exactly like the desktop shell. That is the whole point
// of keeping internal/zen free of desktop dependencies.
//
// gomobile only supports primitive signatures, so every entry point trades in
// (string, int, error), and JSON travels as a string.
package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"

	"zenui/internal/host"
	"zenui/internal/zen/agents"
	"zenui/internal/zen/core"
	"zenui/internal/zen/gateway"
	"zenui/internal/zenbridge"
)

var (
	mu      sync.Mutex
	running *core.Core
	center  *host.Center
)

// Start boots the gateway with homeDir as its data directory (the app private
// filesDir on Android) and port as the loopback listen port (0 keeps the
// configured default).
//
// DisableDesktop crops the features that only make sense on a desktop host:
// agent-config injection, the self-update check and the sing-box subscription
// apply loop. NoTray sends the log to stdout, which on Android is logcat.
func Start(homeDir string, port int) error {
	mu.Lock()
	defer mu.Unlock()
	if running != nil {
		return errors.New("zen-gate: already running")
	}
	if strings.TrimSpace(homeDir) == "" {
		return errors.New("zen-gate: homeDir is empty")
	}

	c, err := core.Start(context.Background(), core.Options{
		Home:           homeDir,
		Port:           port,
		NoTray:         true,
		DisableDesktop: true,
	})
	if err != nil {
		return err
	}

	// The page data door. host.Center owns the envelope shape the UI already
	// understands; zenbridge owns the in-process route into the gateway own
	// admin handlers. Nothing is reimplemented here.
	ctr := host.New()
	ctr.SetGateway(zenbridge.New(c.Gateway))

	running, center = c, ctr
	return nil
}

// Stop shuts the gateway down and flushes stats. Safe to call when stopped.
func Stop() {
	mu.Lock()
	defer mu.Unlock()
	if running != nil {
		running.Stop()
		running = nil
		center = nil
	}
}

// Port is the loopback port the gateway actually listens on, or 0 when the
// service is not running.
func Port() int {
	mu.Lock()
	defer mu.Unlock()
	if running == nil {
		return 0
	}
	base := running.BaseURL() // http://127.0.0.1:PORT/v1
	_, rest, _ := strings.Cut(base, "://127.0.0.1:")
	hostPart, _, _ := strings.Cut(rest, "/")
	p, _ := strconv.Atoi(hostPart)
	return p
}

// StatusURL is the gateway loopback address (no /v1 suffix). The app loads its
// UI from the APK assets, so this is informational: it is what the 服务 page
// shows as the Base URL and what an API client would call.
func StatusURL() string {
	mu.Lock()
	defer mu.Unlock()
	if running == nil {
		return ""
	}
	return running.DashboardURL()
}

// BaseURL is the OpenAI-compatible endpoint (with /v1), or empty when stopped.
func BaseURL() string {
	mu.Lock()
	defer mu.Unlock()
	if running == nil {
		return ""
	}
	return running.BaseURL()
}

// SetPeers records what the app layer observed about sibling apps on this
// device (today: DSH Go). The Agent page cannot answer "is that app installed"
// from its own side — a separate APK's files are not readable here — so the
// Kotlin host asks PackageManager and hands the answer over.
//
// It is a plain fact, not a command: nothing is injected or written. An empty
// payload clears the record, which the page renders as "未知" rather than
// "未安装".
func SetPeers(raw string) { agents.SetPeersJSON(raw) }

// Version is the gateway build string, for the About row.
func Version() string { return gateway.Version }

// IsRunning reports whether the gateway is up; the Kotlin service uses it to
// decide between start and re-bind after START_STICKY recreation.
func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return running != nil
}

// Admin is the page single door into the gateway, mirroring the desktop shell
// "Gateway" method.
//
//	path "state"                  -> GET  /admin/api/state
//	path "settings" + body {...} -> POST /admin/api/settings
//
// It returns the same envelope the desktop host returns,
//
//	{"ok":true,"data":...}  |  {"ok":false,"code":...,"error":"..."}
//
// so the page needs no second interpretation layer for the Android host.
// bodyJSON may be empty or "null" for a GET.
func Admin(path string, bodyJSON string, saveTo string, loadFrom string) string {
	mu.Lock()
	ctr := center
	mu.Unlock()
	if ctr == nil {
		return envelope(gatewayDown())
	}

	payload := map[string]any{"path": path}
	if saveTo != "" {
		payload["saveTo"] = saveTo
	}
	if loadFrom != "" {
		payload["loadFrom"] = loadFrom
	}
	if strings.TrimSpace(bodyJSON) != "" && bodyJSON != "null" {
		// Kept as raw JSON: the host decodes it once, the same way it decodes
		// the desktop bridge payload.
		var body json.RawMessage
		if err := json.Unmarshal([]byte(bodyJSON), &body); err != nil {
			return envelope(fail("bad_payload", "body 不是合法 JSON："+err.Error()))
		}
		payload["body"] = body
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return envelope(fail("bad_payload", "参数无法序列化："+err.Error()))
	}
	out, err := ctr.Dispatch("gateway", raw)
	if err != nil {
		return envelope(fail("io_error", "网关调用失败："+err.Error()))
	}
	return string(out)
}

// envel is the same shape as host.Envelope, declared locally so this package
// can build failures without exporting host internals to Java.
type envel struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

func ok(data any) envel { return envel{OK: true, Data: data} }

func fail(code, msg string) envel { return envel{OK: false, Code: code, Error: msg} }

func gatewayDown() envel { return fail("gateway_down", "网关没有启动") }

func envelope(v envel) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{\"ok\":false,\"code\":\"io_error\",\"error\":\"回复无法序列化\"}"
	}
	return string(b)
}
