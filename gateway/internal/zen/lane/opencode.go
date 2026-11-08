package lane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"zenui/internal/zen/chprov"
)

const OpenCodeAnonymousID = "zen-anonymous"

// OpenCodeState is observed request state, never an estimate of remaining credit.
type OpenCodeState struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
	Until  int64  `json:"until,omitempty"`
	At     int64  `json:"at"`
}

type openCodeSlot struct {
	account  chprov.Account
	identity string
	states   map[string]OpenCodeState
	client   *http.Client
	inFlight int
}

type openCodeRequest struct {
	account chprov.Account
	client  *http.Client
}
type openCodeRequestKey struct{}

func DefaultOpenCodeAccount() chprov.Account {
	return chprov.Account{ID: OpenCodeAnonymousID, Nickname: "默认匿名通道", AccessToken: "public", Source: "builtin"}
}

func OpenCodeIdentity(a chprov.Account) string {
	s := sha256.Sum256([]byte(a.AccessToken + "\x00" + a.Nickname + "\x00" + a.Extra["oc_proxy_mode"] + "\x00" + a.Extra["oc_proxy_url"] + "\x00" + a.Extra["oc_disabled"] + "\x00" + a.Extra["oc_revision"]))
	return hex.EncodeToString(s[:])
}

// NormalizeOpenCodeProxy validates explicitly; invalid proxies never fall back to direct.
func NormalizeOpenCodeProxy(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("代理地址无法解析")
	}
	if u.Scheme == "socks5h" {
		u.Scheme = "socks5"
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" {
		return "", fmt.Errorf("仅支持 HTTP、HTTPS、SOCKS5 代理")
	}
	p, err := strconv.Atoi(u.Port())
	if u.Hostname() == "" || err != nil || p < 1 || p > 65535 || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("请填写有效的主机和端口，例如 http://127.0.0.1:7897")
	}
	u.Path = ""
	return u.String(), nil
}

func OpenCodeProxyLabel(a chprov.Account) string {
	switch a.Extra["oc_proxy_mode"] {
	case "direct":
		return "直连"
	case "custom":
		u, err := url.Parse(a.Extra["oc_proxy_url"])
		if err != nil || u.Host == "" {
			return "代理配置无效"
		}
		return u.Scheme + "://" + u.Host
	default:
		return "跟随全局代理"
	}
}

func OpenCodeClient(a chprov.Account) (*http.Client, error) {
	mode := a.Extra["oc_proxy_mode"]
	if mode == "" || mode == "inherit" {
		return laneHTTP, nil
	}
	t := &http.Transport{ResponseHeaderTimeout: responseHeaderTimeout}
	if mode == "custom" {
		raw, err := NormalizeOpenCodeProxy(a.Extra["oc_proxy_url"])
		if err != nil {
			return nil, err
		}
		u, _ := url.Parse(raw)
		t.Proxy = http.ProxyURL(u)
	} else if mode != "direct" {
		return nil, fmt.Errorf("未知代理模式")
	}
	return &http.Client{Transport: t}, nil
}

func (l *Lane) SetOpenCodePersist(fn func(chprov.Account, map[string]OpenCodeState)) {
	l.ocMu.Lock()
	defer l.ocMu.Unlock()
	l.ocPersist = fn
}

// SetOpenCodeAccounts publishes independent immutable credential snapshots.
func (l *Lane) SetOpenCodeAccounts(accounts chprov.Accounts) {
	l.ocMu.Lock()
	defer l.ocMu.Unlock()
	l.ocConfigured = len(accounts) > 0
	old := map[string]*openCodeSlot{}
	for _, s := range l.ocSlots {
		old[s.account.Key()] = s
	}
	var next []*openCodeSlot
	hasDefault := false
	for _, a := range accounts {
		hasDefault = hasDefault || a.ID == OpenCodeAnonymousID
	}
	if !hasDefault {
		accounts = append(append(chprov.Accounts(nil), accounts...), DefaultOpenCodeAccount())
	}
	// The built-in anonymous fallback stays last regardless of saved ordering.
	for _, a := range accounts {
		if a.ID != OpenCodeAnonymousID {
			next = append(next, l.openCodeSlot(a, old))
		}
	}
	for _, a := range accounts {
		if a.ID == OpenCodeAnonymousID {
			next = append(next, l.openCodeSlot(a, old))
		}
	}
	for key, s := range old {
		kept := false
		for _, n := range next {
			kept = kept || (key == n.account.Key() && s == n)
		}
		if !kept && s.client != nil && s.client != laneHTTP {
			s.client.CloseIdleConnections()
		}
	}
	l.ocSlots = next
}

func (l *Lane) openCodeSlot(a chprov.Account, old map[string]*openCodeSlot) *openCodeSlot {
	id := OpenCodeIdentity(a)
	if s := old[a.Key()]; s != nil && s.identity == id {
		return s
	}
	a.Extra = cloneOpenCodeExtra(a.Extra)
	s := &openCodeSlot{account: a, identity: id, states: map[string]OpenCodeState{}}
	_ = json.Unmarshal([]byte(a.Extra["oc_states"]), &s.states)
	if s.states == nil {
		s.states = map[string]OpenCodeState{}
	}
	s.client, _ = OpenCodeClient(a)
	return s
}

func cloneOpenCodeExtra(src map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range src {
		out[k] = v
	}
	return out
}

func (l *Lane) openCodeConfigured() bool {
	l.ocMu.Lock()
	defer l.ocMu.Unlock()
	return l.ocConfigured
}

func (l *Lane) OpenCodeCanServe(model string) bool {
	if IsFreeLane(model) {
		return true
	}
	l.ocMu.Lock()
	defer l.ocMu.Unlock()
	for _, s := range l.ocSlots {
		if s.account.AccessToken != "" && s.account.AccessToken != "public" && s.account.Extra["oc_disabled"] != "true" && s.client != nil {
			return true
		}
	}
	return false
}

func (l *Lane) managedOpenCodeModel(model string) bool {
	if !l.openCodeConfigured() {
		return false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, m := range l.catalog {
		if m.ID == BaseModelId(model) {
			return m.Channel == ""
		}
	}
	return true
}

func (l *Lane) OpenCodeViews() []map[string]any {
	l.ocMu.Lock()
	defer l.ocMu.Unlock()
	out := []map[string]any{}
	for _, s := range l.ocSlots {
		a := s.account
		states := map[string]OpenCodeState{}
		for k, v := range s.states {
			states[k] = v
		}
		mode := a.Extra["oc_proxy_mode"]
		if mode == "" {
			mode = "inherit"
		}
		out = append(out, map[string]any{"key": a.Key(), "id": a.ID, "label": a.Label(), "anonymous": a.AccessToken == "public", "builtin": a.ID == OpenCodeAnonymousID,
			"enabled": a.Extra["oc_disabled"] != "true", "proxyMode": mode, "proxyLabel": OpenCodeProxyLabel(a), "proxySaved": a.Extra["oc_proxy_url"] != "", "states": states, "inFlight": s.inFlight})
	}
	return out
}

func (l *Lane) OpenCodeEnabledCount() int {
	l.ocMu.Lock()
	defer l.ocMu.Unlock()
	n := 0
	for _, s := range l.ocSlots {
		if s.account.Extra["oc_disabled"] != "true" && s.client != nil && s.account.AccessToken != "" {
			n++
		}
	}
	return n
}

func (l *Lane) openCodeSaveState(s *openCodeSlot, model string, state OpenCodeState) {
	l.ocMu.Lock()
	defer l.ocMu.Unlock()
	for _, current := range l.ocSlots {
		if current != s {
			continue
		} // Logout/reset/proxy edits invalidate in-flight results.
		s.states[model] = state
		if len(s.states) > 200 {
			for k, v := range s.states {
				if v.At < time.Now().Add(-24*time.Hour).UnixMilli() {
					delete(s.states, k)
				}
			}
		}
		if l.ocPersist != nil {
			snapshot := map[string]OpenCodeState{}
			for k, v := range s.states {
				snapshot[k] = v
			}
			l.ocPersist(s.account, snapshot)
		}
		return
	}
}

func openCodeResult(a chprov.Account, err error) OpenCodeState {
	s := OpenCodeState{State: StateAvailable, At: time.Now().UnixMilli()}
	if err == nil {
		return s
	}
	ue := asUpstream(err)
	s.State = StateUnknown
	// Network errors can contain proxy passwords; only expose a stable error class.
	s.Detail = "请求失败（" + ue.Code + "）"
	switch ue.Code {
	case CodeQuota:
		s.State = StateThrottled
		wait := ue.RetryAfter
		if wait <= 0 {
			wait = 60
		}
		s.Until = time.Now().Add(time.Duration(wait) * time.Second).UnixMilli()
	case CodeCredential:
		s.State = StateUnavailable
		s.Detail = "凭据被上游拒绝"
		s.Until = time.Now().Add(5 * time.Minute).UnixMilli()
	case CodeRegion:
		s.State = StateRegionBlock
		s.Detail = "当前出口地区受限"
		s.Until = time.Now().Add(time.Minute).UnixMilli()
	}
	return s
}

func (l *Lane) openCodePost(ctx context.Context, path string, body map[string]any, session, requestID string, onData func([]byte) error) (*Usage, error) {
	if !l.openCodeConfigured() {
		return PostStreamed(ctx, path, body, session, requestID, onData)
	}
	model, _ := body["model"].(string)
	tried := map[string]bool{}
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		if ctx.Err() != nil {
			return nil, &UpstreamError{Code: CodeAborted, Message: "cancelled"}
		}
		l.ocMu.Lock()
		var picked *openCodeSlot
		for _, s := range l.ocSlots {
			if tried[s.account.Key()] || s.account.Extra["oc_disabled"] == "true" || s.account.AccessToken == "" || s.client == nil {
				continue
			}
			if s.account.AccessToken == "public" && !IsFreeLane(model) {
				continue
			}
			if st := s.states[model]; st.Until > time.Now().UnixMilli() {
				continue
			}
			if picked == nil || s.inFlight < picked.inFlight {
				picked = s
			}
		}
		if picked != nil {
			picked.inFlight++
		}
		l.ocMu.Unlock()
		if picked == nil {
			break
		}
		tried[picked.account.Key()] = true
		cctx := context.WithValue(ctx, openCodeRequestKey{}, openCodeRequest{picked.account, picked.client})
		accountSession := session
		if picked.account.ID != OpenCodeAnonymousID {
			accountSession = SessionForConversation(session + ":" + picked.account.Key())
		}
		if mode := picked.account.Extra["oc_proxy_mode"]; mode == "" || mode == "inherit" {
			if rotator := CurrentRotator(); rotator != nil {
				plan := ExitPlan{Model: model}
				plan.NodeID = rotator.Pick(accountSession, model)
				plan.ExitKey = rotator.ExitKey(plan.NodeID)
				cctx = WithExitPlan(cctx, plan)
				accountSession = SessionForExit(accountSession, plan.ExitKey)
			}
		}
		saw := false
		u, err := PostStreamed(cctx, path, body, accountSession, RequestIdFor(accountSession, requestID), func(p []byte) error { saw = true; return onData(p) })
		l.ocMu.Lock()
		picked.inFlight--
		l.ocMu.Unlock()
		if ctx.Err() == nil {
			l.openCodeSaveState(picked, model, openCodeResult(picked.account, err))
		}
		if err == nil || saw || ctx.Err() != nil {
			return u, err
		}
		ue := asUpstream(err)
		if ue.Code != CodeQuota && ue.Code != CodeCredential && ue.Code != CodeRegion && ue.Code != CodeTransport && ue.Code != CodeServer {
			return u, err
		}
		last = err
	}
	if last != nil {
		return nil, last
	}
	return nil, &UpstreamError{Code: CodeQuota, Message: "OpenCode 通道已停用或该模型仍在冷却", RetryAfter: 30}
}

// ProbeOpenCode bypasses local cooldown for exactly one explicitly selected slot.
func (l *Lane) ProbeOpenCode(ctx context.Context, key, model string) (OpenCodeState, error) {
	l.ocMu.Lock()
	var picked *openCodeSlot
	for _, s := range l.ocSlots {
		if s.account.Key() == key {
			picked = s
			break
		}
	}
	l.ocMu.Unlock()
	if picked == nil {
		return OpenCodeState{}, fmt.Errorf("账号不存在")
	}
	if picked.client == nil {
		return OpenCodeState{}, fmt.Errorf("代理配置无效")
	}
	if picked.account.AccessToken == "" {
		return OpenCodeState{}, fmt.Errorf("账号缺少凭据")
	}
	cat, _, _ := l.Snapshot()
	var entry *ModelInfo
	for i := range cat {
		if cat[i].ID == model && cat[i].Channel == "" && !cat[i].SystemOne {
			entry = &cat[i]
			break
		}
	}
	if entry == nil {
		return OpenCodeState{}, fmt.Errorf("请选择 OpenCode 目录中的对话模型")
	}
	cctx := context.WithValue(ctx, openCodeRequestKey{}, openCodeRequest{picked.account, picked.client})
	r := ProbeModel(cctx, *entry)
	s := OpenCodeState{State: r.State, Detail: r.State, At: r.At}
	if r.State == StateThrottled {
		s.Until = time.Now().Add(time.Minute).UnixMilli()
	}
	l.openCodeSaveState(picked, model, s)
	if r.State == StateAvailable {
		l.NoteRealSuccess(model)
	}
	return s, nil
}
