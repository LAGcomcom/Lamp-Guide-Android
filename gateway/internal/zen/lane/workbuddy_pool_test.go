package lane

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"zenui/internal/zen/chprov"
)

func TestWorkBuddyPoolQuotaSwitchIsStickyAndPreservesModel(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		calls = append(calls, auth)
		mu.Unlock()
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		if !strings.Contains(body, `"model":"same-model"`) {
			t.Errorf("wrong model: %s", body)
		}
		if auth == "Bearer a" {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"code":6004}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, wbChatSSE(`{"choices":[{"delta":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	old := WorkBuddyBase
	WorkBuddyBase = srv.URL
	t.Cleanup(func() { WorkBuddyBase = old })
	a := wbAcct()
	a.UID = "a"
	a.AccessToken = "a"
	b := a
	b.UID = "b"
	b.AccessToken = "b"
	l := NewLane()
	l.SetWorkBuddyAccounts([]WBAccount{a, b})
	for range 2 {
		if _, err := l.wbPost(context.Background(), map[string]any{"model": "same-model"}, "c", func([]byte) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(calls, ",") != "Bearer a,Bearer b,Bearer b" {
		t.Fatalf("account sequence = %v", calls)
	}
	if !time.Now().Before(l.wbPool[0].cool["same-model"]) || !l.wbPool[1].cool["same-model"].IsZero() {
		t.Fatal("cooldown crossed account boundary")
	}
}

func TestWorkBuddyPoolDoesNotReplayDeliveredStream(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"half\"}}]}\n\n")
	}))
	defer srv.Close()
	old := WorkBuddyBase
	WorkBuddyBase = srv.URL
	t.Cleanup(func() { WorkBuddyBase = old })
	a := wbAcct()
	b := a
	b.UID = "b"
	b.AccessToken = "b"
	l := NewLane()
	l.SetWorkBuddyAccounts([]WBAccount{a, b})
	_, _ = l.wbPost(context.Background(), map[string]any{"model": "m"}, "c", func([]byte) error { return fmt.Errorf("caller stopped") })
	if calls != 1 {
		t.Fatalf("replayed delivered stream %d times", calls)
	}
}

func TestWorkBuddyPoolRefreshIdentityAndRemovedAccount(t *testing.T) {
	src := chprov.Account{AccessToken: wbJWT("u-a", "https://www.workbuddy.ai", 1900000000), Extra: map[string]string{"uid": "u-a"}}
	a := WBAccountFromChannel(chprov.Accounts{src}, WBAccount{})
	if a.ChannelKey != src.Key() {
		t.Fatal("lost source account key")
	}
	b := wbAcct()
	b.UID = "b"
	l := NewLane()
	l.SetWorkBuddyAccounts([]WBAccount{a, b})
	var saved []WBAccount
	l.SetWorkBuddyPersist(func(a WBAccount) { saved = append(saved, a) })
	oldPersist := l.wbPool[0].client.wbPersist
	fresh := a
	fresh.AccessToken = "rotated"
	oldPersist(fresh)
	if len(saved) != 1 || saved[0].ChannelKey != src.Key() || l.wbPool[1].seed.AccessToken != b.AccessToken {
		t.Fatal("refresh changed wrong identity")
	}
	l.SetWorkBuddyAccounts([]WBAccount{b})
	oldPersist(fresh)
	if len(saved) != 1 {
		t.Fatal("removed account was persisted again")
	}
}

func TestWorkBuddyExplicitLoginUsesLoginWriter(t *testing.T) {
	token := wbJWT("new-user", "https://www.workbuddy.ai", 1900000000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			fmt.Fprint(w, `{"data":[{"id":"mimo-v2.6-flash-free","isFree":true}]}`)
			return
		}
		if strings.Contains(r.URL.Path, wbAuthTokenPath) {
			fmt.Fprintf(w, `{"code":0,"data":{"accessToken":%q,"refreshToken":"new-r"}}`, token)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":{"nickname":"new-user"}}`)
	}))
	defer srv.Close()
	old := WorkBuddyBase
	WorkBuddyBase = srv.URL
	t.Cleanup(func() { WorkBuddyBase = old })
	oldUp, oldKilo := UpstreamBase, KiloBase
	UpstreamBase, KiloBase = srv.URL, srv.URL
	t.Cleanup(func() { UpstreamBase, KiloBase = oldUp, oldKilo })
	l := NewLane()
	refreshed := make(chan struct{}, 1)
	l.OnCatalogReload = func() { refreshed <- struct{}{} }
	l.wbLoginState = "login-state"
	l.wbLoginAt = time.Now()
	refreshCalls, loginCalls := 0, 0
	l.SetWorkBuddyPersist(func(WBAccount) { refreshCalls++ })
	l.SetWorkBuddyLoginPersist(func(a WBAccount) {
		loginCalls++
		if a.UID != "new-user" {
			t.Error("wrong login identity")
		}
	})
	state, err := l.WorkBuddyLoginPoll()
	select {
	case <-refreshed:
	case <-time.After(10 * time.Second):
		t.Fatal("catalog refresh did not finish")
	}
	if err != nil || state != "ok" || refreshCalls != 0 || loginCalls != 1 {
		t.Fatalf("state=%s err=%v refresh=%d login=%d", state, err, refreshCalls, loginCalls)
	}
}
