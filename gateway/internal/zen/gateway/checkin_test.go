package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"zenui/internal/zen/checkin"
	"zenui/internal/zen/chprov"
	"zenui/internal/zen/store"
)

func checkinAdmin(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1/admin/api/"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	return w
}

func TestCheckinAdminSettingsAndPrivacy(t *testing.T) {
	up := fakeUpstream(t, nil)
	defer up.Close()
	s := newTestServer(t, up)
	s.Store.Mutate(func(c *store.Config) {
		c.ChannelAccounts = map[string]chprov.Accounts{
			"minimax": {{ID: "test-id", AccessToken: "NEVER-RETURN-ACCESS", RefreshTok: "NEVER-RETURN-REFRESH"}},
			"zcode":   {{ID: "z-id", AccessToken: "NEVER-RETURN-ZCODE"}},
			"qoder":   {{ID: "q-id", AccessToken: "NEVER-RETURN-QODER"}},
			"qodercn": {{ID: "q-id", AccessToken: "NEVER-RETURN-QODER"}},
		}
	})
	w := checkinAdmin(s, "GET", "checkin/state", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "NEVER-RETURN") {
		t.Fatal(w.Code, "secret appeared in checkin state")
	}
	var view checkin.View
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Accounts) != 3 {
		t.Fatalf("alias duplicated account: %d", len(view.Accounts))
	}
	for _, row := range view.Accounts {
		if row.Channel == "zcode" && row.Mode != "manual" {
			t.Fatal("captcha channel marked automatic")
		}
	}
	w = checkinAdmin(s, "POST", "checkin/settings", `{"enabled":false,"times":["21:00","09:00"],"disabled":{"minimax":true}}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if s.checkins.Snapshot().Settings.Enabled || !s.checkins.Snapshot().Settings.Disabled["minimax"] {
		t.Fatal("settings not saved")
	}
	for _, body := range []string{`{"enabled":true,"times":["99:00"]}`, `{"times":["09:00"],"disabled":{"unknown":true}}`, `{"times":["09:00"]} {}`, `{"times":["09:00"],"token":"invalid-field"}`} {
		if w := checkinAdmin(s, "POST", "checkin/settings", body); w.Code != 400 {
			t.Fatal("invalid settings accepted", w.Code)
		}
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1/admin/api/checkin/run", strings.NewReader(`{}`))
	r.RemoteAddr = "192.0.2.5:123"
	w = httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("nonlocal admin checkin accepted")
	}
}

func TestCheckinTriggerIsAsyncAndSingleFlight(t *testing.T) {
	up := fakeUpstream(t, nil)
	defer up.Close()
	s := newTestServer(t, up)
	entered, release := make(chan struct{}), make(chan struct{})
	s.checkins = checkin.New(s.Store.Home, func() []checkin.Target {
		return []checkin.Target{{Channel: "buddy", Key: "fake-key", Mode: "automatic"}}
	}, func(ctx context.Context, _ checkin.Target, day string) checkin.Outcome {
		close(entered)
		<-release
		return checkin.Outcome{Status: "claimed", CoveredDay: day}
	})
	s.checkinCtx = context.Background()
	if w := checkinAdmin(s, "POST", "checkin/run", `{}`); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	<-entered
	if w := checkinAdmin(s, "POST", "checkin/run", `{}`); w.Code != 409 {
		t.Fatal("overlapping run accepted", w.Code)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for s.checkins.Snapshot().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.checkins.Snapshot().Running {
		t.Fatal("run did not complete")
	}
}

func TestRotatingTokensKeepOpaqueCheckinIdentity(t *testing.T) {
	up := fakeUpstream(t, nil)
	defer up.Close()
	s := newTestServer(t, up)
	s.Store.Mutate(func(c *store.Config) {
		c.ChannelAccounts = map[string]chprov.Accounts{"minimaxintl": {{AccessToken: "fake-old", RefreshTok: "fake-rt", ExpiresAtMS: 1}}}
	})
	target := s.checkinTargets()[0]
	s.checkinClient = &http.Client{Transport: reviewQuotaRT(func(r *http.Request) (*http.Response, error) {
		body := `{"base_resp":{"status_code":0},"data":{"claim_result":1,"points":5}}`
		if r.URL.Path == "/oauth2/token" {
			body = `{"access_token":"fake-next","refresh_token":"fake-rt-next","expires_in":3600}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})}
	out := s.claimAccount(context.Background(), target, "2026-11-06")
	if out.Status != "claimed" {
		t.Fatal(out)
	}
	next := s.checkinTargets()
	if len(next) != 1 || next[0].Key != target.Key {
		t.Fatal("token rotation changed daily identity")
	}
	if s.Store.Config().ChannelAccounts["minimaxintl"][0].AccessToken != "fake-next" {
		t.Fatal("refreshed credential not saved")
	}
}
