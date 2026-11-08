package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"zenui/internal/zen/lane"
)

func TestOpenCodeSavedKeyReachesRealCompletionRoute(t *testing.T) {
	token := "test-opencode-key-123456789"
	got := make(chan string, 2)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer up.Close()
	s := newTestServer(t, up)
	ocAPI(t, s, "add", map[string]any{"token": token}, 200)
	a := s.Store.Config().ChannelAccounts["opencode"][0]
	ocAPI(t, s, "settings", map[string]any{"key": a.Key(), "proxyMode": "direct"}, 200)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.Lane.Complete(ctx, lane.Request{Model: "big-pickle", PreserveModel: true, Messages: []lane.Message{{Role: lane.RoleUser, Parts: []lane.Part{lane.TextPart{Text: "test"}}}}}, func(lane.Chunk) {})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case auth := <-got:
		if auth != "Bearer "+token {
			t.Fatalf("saved key not used: %s", auth)
		}
	default:
		t.Fatal("completion did not reach mock upstream")
	}
	if s.Store.Config().ChannelAccounts["opencode"][0].Extra["oc_states"] == "" {
		t.Fatal("completion state was not persisted")
	}
}

func ocAPI(t *testing.T, s *Server, path string, body any, want int) map[string]any {
	t.Helper()
	encoded, _ := json.Marshal(body)
	method := http.MethodPost
	if body == nil {
		method = http.MethodGet
	}
	r := httptest.NewRequest(method, "http://127.0.0.1/admin/api/opencode/"+path, strings.NewReader(string(encoded)))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s: status=%d body=%s", path, w.Code, w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

func TestOpenCodeDedicatedSettingsPersistWithoutExposingSecrets(t *testing.T) {
	up := fakeUpstream(t, []string{"[DONE]"})
	defer up.Close()
	s := newTestServer(t, up)
	token := "test-secret-account-key-123456789"
	ocAPI(t, s, "add", map[string]any{"token": token, "nickname": "工作账号"}, 200)
	accounts := s.Store.Config().ChannelAccounts["opencode"]
	if len(accounts) != 1 {
		t.Fatal("account was not saved")
	}
	key := accounts[0].Key()
	ocAPI(t, s, "settings", map[string]any{"key": key, "proxyMode": "custom", "proxyUrl": "http://name:password@127.0.0.1:7897"}, 200)
	out := ocAPI(t, s, "state", nil, 200)
	encoded, _ := json.Marshal(out)
	if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), "name:") {
		t.Fatal("state leaked credentials")
	}
	ocAPI(t, s, "settings", map[string]any{"key": key, "proxyMode": "custom"}, 200)
	if s.Store.Config().ChannelAccounts["opencode"][0].Extra["oc_proxy_url"] != "http://name:password@127.0.0.1:7897" {
		t.Fatal("blank proxy edit erased saved proxy")
	}
	ln := lane.NewLane()
	ln.SetOpenCodeAccounts(s.Store.Config().ChannelAccounts["opencode"])
	if len(ln.OpenCodeViews()) != 2 {
		t.Fatal("saved account did not restore with anonymous fallback")
	}
	ocAPI(t, s, "settings", map[string]any{"key": key, "enabled": false}, 200)
	ocAPI(t, s, "settings", map[string]any{"key": "id:" + lane.OpenCodeAnonymousID, "enabled": false}, 200)
	if s.aggAccounts("opencode") != 0 {
		t.Fatal("all disabled accounts remained eligible")
	}
	ocAPI(t, s, "delete", map[string]any{"key": key}, 200)
	if len(s.Store.Config().ChannelAccounts["opencode"]) != 1 {
		t.Fatal("account removal lost anonymous preferences")
	}
}

func TestOpenCodeInvalidSettingsDoNotReplaceWorkingProxy(t *testing.T) {
	up := fakeUpstream(t, []string{"[DONE]"})
	defer up.Close()
	s := newTestServer(t, up)
	key := "id:" + lane.OpenCodeAnonymousID
	ocAPI(t, s, "settings", map[string]any{"key": key, "proxyMode": "custom", "proxyUrl": "http://127.0.0.1:7897"}, 200)
	ocAPI(t, s, "settings", map[string]any{"key": key, "proxyMode": "custom", "proxyUrl": "file:///bad"}, 400)
	if s.Store.Config().ChannelAccounts["opencode"][0].Extra["oc_proxy_url"] != "http://127.0.0.1:7897" {
		t.Fatal("invalid edit destroyed working settings")
	}
	ocAPI(t, s, "add", map[string]any{"token": "test-secret-key-1234567890"}, 200)
	ocAPI(t, s, "add", map[string]any{"token": "test-secret-key-1234567890"}, 400)
}

func TestOpenCodeResetPreventsStalePersistence(t *testing.T) {
	up := fakeUpstream(t, []string{"[DONE]"})
	defer up.Close()
	s := newTestServer(t, up)
	ocAPI(t, s, "add", map[string]any{"token": "test-secret-key-1234567890"}, 200)
	a := s.Store.Config().ChannelAccounts["opencode"][0]
	s.persistOpenCode(a, map[string]lane.OpenCodeState{"m": {State: lane.StateThrottled, Until: 9999999999999}})
	ocAPI(t, s, "reset", map[string]any{"key": a.Key()}, 200)
	s.persistOpenCode(a, map[string]lane.OpenCodeState{"m": {State: lane.StateThrottled, Until: 9999999999999}})
	if s.Store.Config().ChannelAccounts["opencode"][0].Extra["oc_states"] != "" {
		t.Fatal("stale result overwrote cleared state")
	}
	ocAPI(t, s, "delete", map[string]any{"key": a.Key()}, 200)
	s.persistOpenCode(a, map[string]lane.OpenCodeState{"m": {State: lane.StateAvailable}})
	if len(s.Store.Config().ChannelAccounts["opencode"]) != 0 {
		t.Fatal("late result resurrected removed account")
	}
}
