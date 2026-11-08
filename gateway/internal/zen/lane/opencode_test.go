package lane

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zenui/internal/zen/chprov"
)

func ocTestAccount(id, token string) chprov.Account {
	return chprov.Account{ID: id, AccessToken: token, Extra: map[string]string{"oc_proxy_mode": "direct"}}
}
func ocTestLane(t *testing.T, handler http.HandlerFunc, accounts chprov.Accounts) *Lane {
	t.Helper()
	up := httptest.NewServer(handler)
	t.Cleanup(up.Close)
	old := UpstreamBase
	UpstreamBase = up.URL
	t.Cleanup(func() { UpstreamBase = old })
	l := NewLane()
	l.SetOpenCodeAccounts(accounts)
	return l
}
func ocPost(l *Lane, model string, onData func([]byte) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := l.openCodePost(ctx, "/test", map[string]any{"model": model}, "session", "request", onData)
	return err
}
func ocOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
}

func TestOpenCodeUsesAccountKeyAndKeepsModelCooldownSeparate(t *testing.T) {
	var calls []string
	var mu sync.Mutex
	l := ocTestLane(t, func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Authorization")
		mu.Lock()
		calls = append(calls, key)
		mu.Unlock()
		if key == "Bearer first" {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":"rate limit"}`)
			return
		}
		ocOK(w)
	}, chprov.Accounts{ocTestAccount("a", "first"), ocTestAccount("b", "second")})
	if err := ocPost(l, "model-a", func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := ocPost(l, "model-a", func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := ocPost(l, "model-b", func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(calls, ",") != "Bearer first,Bearer second,Bearer second,Bearer first,Bearer second" {
		t.Fatalf("wrong account routing: %v", calls)
	}
}

func TestOpenCodeCustomProxyActuallyCarriesCredential(t *testing.T) {
	var proxyCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer secret-key" || r.URL.Host != "opencode.invalid" {
			t.Errorf("proxy did not receive account request: %s %s", r.Header.Get("Authorization"), r.URL.Host)
		}
		ocOK(w)
	}))
	defer proxy.Close()
	old := UpstreamBase
	UpstreamBase = "http://opencode.invalid"
	defer func() { UpstreamBase = old }()
	a := ocTestAccount("a", "secret-key")
	a.Extra["oc_proxy_mode"] = "custom"
	a.Extra["oc_proxy_url"] = proxy.URL
	l := NewLane()
	l.SetOpenCodeAccounts(chprov.Accounts{a})
	if err := ocPost(l, "m", func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if proxyCalls.Load() != 1 {
		t.Fatalf("custom proxy calls: %d", proxyCalls.Load())
	}
}

func TestOpenCodeNeverReplaysAfterStreamData(t *testing.T) {
	var second atomic.Int32
	l := ocTestLane(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer second" {
			second.Add(1)
		}
		ocOK(w)
	}, chprov.Accounts{ocTestAccount("a", "first"), ocTestAccount("b", "second")})
	err := ocPost(l, "m", func([]byte) error { return &UpstreamError{Code: CodeQuota, Message: "refusal after content"} })
	if err == nil || second.Load() != 0 {
		t.Fatalf("stream was replayed: err=%v calls=%d", err, second.Load())
	}
}

func TestOpenCodeResetDiscardsOldInFlightResult(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	a := ocTestAccount("a", "first")
	defaultAccount := DefaultOpenCodeAccount()
	defaultAccount.Extra = map[string]string{"oc_disabled": "true"}
	l := ocTestLane(t, func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(429)
		fmt.Fprint(w, "rate limit")
	}, chprov.Accounts{a, defaultAccount})
	done := make(chan error, 1)
	go func() { done <- ocPost(l, "m", func([]byte) error { return nil }) }()
	<-started
	a.Extra["oc_revision"] = "reset"
	l.SetOpenCodeAccounts(chprov.Accounts{a, defaultAccount})
	close(release)
	<-done
	for _, view := range l.OpenCodeViews() {
		if view["key"] == a.Key() && len(view["states"].(map[string]OpenCodeState)) != 0 {
			t.Fatal("old request resurrected reset state")
		}
	}
}

func TestOpenCodeDisabledSlotsAndBadProxiesDoNotSilentlyRun(t *testing.T) {
	var calls atomic.Int32
	a := ocTestAccount("a", "secret")
	a.Extra["oc_disabled"] = "true"
	d := DefaultOpenCodeAccount()
	d.Extra = map[string]string{"oc_disabled": "true"}
	l := ocTestLane(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); ocOK(w) }, chprov.Accounts{a, d})
	if err := ocPost(l, "m", func([]byte) error { return nil }); err == nil || calls.Load() != 0 {
		t.Fatal("disabled slot sent request")
	}
	a.Extra = map[string]string{"oc_proxy_mode": "custom", "oc_proxy_url": "file:///bad"}
	l.SetOpenCodeAccounts(chprov.Accounts{a, d})
	if err := ocPost(l, "m", func([]byte) error { return nil }); err == nil || calls.Load() != 0 {
		t.Fatal("invalid proxy fell back to direct")
	}
}

func TestOpenCodeConcurrentRequestsKeepCredentialAndProxyTogether(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var first atomic.Int32
	a := ocTestAccount("a", "first")
	b := ocTestAccount("b", "second")
	l := ocTestLane(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer first" {
			if first.Add(1) == 1 {
				close(started)
			}
			<-release
		}
		ocOK(w)
	}, chprov.Accounts{a, b})
	done := make(chan error, 1)
	go func() { done <- ocPost(l, "m", func([]byte) error { return nil }) }()
	<-started
	if err := ocPost(l, "m", func([]byte) error { return nil }); err != nil {
		t.Error(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Error(err)
	}
	if first.Load() != 1 {
		t.Fatal("concurrent request did not select idle second account")
	}
}

func TestOpenCodeProxyValidationAndSafeLabel(t *testing.T) {
	for _, raw := range []string{"file:///bad", "http://host", "http://host:70000", "http://host:80/path", "http://host:80?bad=1"} {
		if _, err := NormalizeOpenCodeProxy(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	raw, err := NormalizeOpenCodeProxy("socks5h://name:secret@127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	a := ocTestAccount("a", "secret")
	a.Extra["oc_proxy_mode"] = "custom"
	a.Extra["oc_proxy_url"] = raw
	label := OpenCodeProxyLabel(a)
	if strings.Contains(label, "name") || strings.Contains(label, "secret") {
		t.Fatalf("credentials leaked: %s", label)
	}
}
