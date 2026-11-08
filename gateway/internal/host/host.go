// Package host is the data layer behind the window: one door per capability the
// zen gateway page needs, and nothing else.
//
// The page never talks to the gateway's HTTP admin API directly. AdminRequest
// is built in-process (see internal/zenbridge), so the dashboard answers from
// the same handler code the HTTP surface does. This package only adds what the
// gateway cannot do for itself: native file dialogs, revealing a folder in
// Explorer, and handing a URL to the browser.
package host

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Error codes. Stable strings: the page shows the human message, tests branch
// on the code.
const (
	CodeUnknownMethod   = "unknown_method"
	CodeBadPayload      = "bad_payload"
	CodeIOError         = "io_error"
	CodeInvalidValue    = "invalid_value"
	CodeParseFailed     = "settings_parse_failed"
	CodeGatewayDown     = "gateway_down"
	CodeGatewayRejected = "gateway_rejected"
)

// Envelope is the single reply shape handed to the page.
type Envelope struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
}

func ok(data any) *Envelope { return &Envelope{OK: true, Data: data} }

func fail(code, message string) *Envelope {
	return &Envelope{OK: false, Code: code, Error: message}
}

// GatewayBridge is the in-process door to the running gateway. Implemented by
// internal/zenbridge; declared here so this package owns no gateway import.
type GatewayBridge interface {
	AdminRequest(path string, body any) (int, []byte, error)
}

// Center owns the running-gateway handle. It holds no configuration of its
// own: the gateway's config.json is the only state.
type Center struct {
	gateway GatewayBridge
}

// Dispatch is the single entry point the shell calls.
func (c *Center) Dispatch(method string, payload json.RawMessage) (json.RawMessage, error) {
	h, found := handlers[strings.ToLower(strings.TrimSpace(method))]
	if !found {
		return json.Marshal(fail(CodeUnknownMethod, "未知的方法："+method))
	}
	out, err := json.Marshal(h(c, payload))
	if err != nil {
		return nil, fmt.Errorf("回复无法序列化: %w", err)
	}
	return out, nil
}

var handlers map[string]func(*Center, json.RawMessage) *Envelope

func init() {
	handlers = map[string]func(*Center, json.RawMessage) *Envelope{
		// the gateway itself: one door, every admin route behind it
		"gateway": (*Center).handleGateway,

		// native shell actions the gateway's admin API cannot do for itself
		"picksavepath": (*Center).handlePickSavePath,
		"pickopenpath": (*Center).handlePickOpenPath,
		"openpath":     (*Center).handleOpenPath,
		"openurl":      (*Center).handleOpenURL,
	}
}

// unmarshalArgs decodes a payload into T, reporting a readable failure when the
// page sent something unexpected.
func unmarshalArgs[T any](raw json.RawMessage) (T, *Envelope) {
	var v T
	if len(raw) == 0 || string(raw) == "null" {
		return v, fail(CodeBadPayload, "缺少参数")
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fail(CodeBadPayload, "请求参数格式不正确："+err.Error())
	}
	return v, nil
}

// New returns a host with no gateway attached; SetGateway fills that in once
// the gateway has started.
func New() *Center { return &Center{} }

// SetGateway wires the running gateway; nil leaves every page reporting it as
// not started instead of pretending.
func (c *Center) SetGateway(b GatewayBridge) { c.gateway = b }
