package lane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"zenui/internal/zen/chprov"
	"zenui/internal/zen/modelprice"
)

func TestWorkBuddyFullCatalogUnionAndFailurePreservesPaidModels(t *testing.T) {
	var failScoped, failConfig atomic.Bool
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("X-Product-Code") != "workbuddy" {
			t.Error("catalog credentials/product missing")
		}
		switch r.URL.Path {
		case "/console/enterprises/personal/models":
			if failScoped.Load() {
				w.WriteHeader(503)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"agents":[{"name":"cli","models":["gpt-6","hy3"]}],"models":[{"id":"gpt-6","name":"GPT-6","credits":"x1.62","maxInputTokens":200000,"supportsImages":true},{"id":"hy3","credits":"x0"},{"id":"completion-hidden","supportsImages":false}]}}`))
		case "/v3/config":
			if failConfig.Load() {
				w.WriteHeader(503)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"agents":[{"name":"craft","models":["hy4-preview-f","gpt-6"]}],"models":[{"id":"hy4-preview-f","credits":"x0.29"},{"id":"gpt-6","credits":"x2"}],"modelPromotions":[{"kind":"discount","enabled":true,"modelIds":["gpt-6"],"discount":{"discountedCredits":"0.50x"}}]}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	old := WorkBuddyBase
	WorkBuddyBase = up.URL
	t.Cleanup(func() { WorkBuddyBase = old })
	l := NewLane()
	l.SetWorkBuddyAccount(WBAccount{UID: "one", Enabled: true, AccessToken: "token"})
	cat := l.fetchWorkBuddyCatalog(context.Background())
	if len(cat) != 3 {
		t.Fatalf("full union = %v", cat)
	}
	var paid ModelInfo
	for _, m := range cat {
		if m.ID == "gpt-6" {
			paid = m
		}
	}
	if paid.Pricing.Rate == nil || *paid.Pricing.Rate != 1.62 || *paid.Pricing.Effective(time.Now()) != .5 || paid.ContextWindow != 200000 {
		t.Fatalf("paid model metadata lost: %+v", paid)
	}
	if paid.BackgroundFree() {
		t.Fatal("paid model allowed for background work")
	}
	_ = l.fetchWorkBuddyCatalog(context.Background())
	if hits.Load() != 2 {
		t.Fatal("catalog cache did not coalesce refresh")
	}
	for _, partial := range []bool{true, false} {
		failScoped.Store(true)
		failConfig.Store(!partial)
		l.wbCatalogAt = time.Time{}
		cat = l.fetchWorkBuddyCatalog(context.Background())
		if len(cat) != 3 {
			t.Fatalf("failure shrank paid catalog: %v", cat)
		}
	}
	l.SetWorkBuddyAccount(WBAccount{UID: "two", Enabled: true, AccessToken: "token"})
	cat = l.fetchWorkBuddyCatalog(context.Background())
	for _, m := range cat {
		if m.ID == "gpt-6" {
			t.Fatal("catalog from previous identity leaked")
		}
	}
}

func TestCompleteCatalogDiscoversPaidModelsButAnonymousCannotServeThem(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "sample-free"}, map[string]any{"id": "gpt-6", "credits": "x1.5"}}})
	}))
	defer up.Close()
	old := UpstreamBase
	UpstreamBase = up.URL
	t.Cleanup(func() { UpstreamBase = old })
	cat, err := FetchOpenCodeCatalog(context.Background())
	if err != nil || len(cat) != 2 {
		t.Fatalf("full Zen roster = %v, %v", cat, err)
	}
	l := NewLane()
	l.catalog = cat
	if l.OpenCodeCanServe("gpt-6") {
		t.Fatal("paid model authorized anonymously")
	}
	if models := l.ServableModels(); len(models) != 1 || models[0].ID != "sample-free" {
		t.Fatalf("unusable paid model injected: %v", models)
	}
	l.SetOpenCodeAccounts(chprov.Accounts{{ID: "paid-account", AccessToken: "key"}})
	if !l.OpenCodeCanServe("gpt-6") || len(l.ServableModels()) != 2 {
		t.Fatal("configured key did not enable paid roster")
	}
	kilo := buildKiloCatalog([]map[string]any{{"id": "free", "isFree": true}, {"id": "paid", "isFree": false}}, true)
	if len(kilo) != 2 || !kilo[1].RequiresKey || kilo[1].BackgroundFree() {
		t.Fatalf("Kilo keyless paid guard failed: %+v", kilo)
	}
}

func TestBackgroundTasksSkipPaidAndUnknownModels(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer up.Close()
	old := WorkBuddyBase
	WorkBuddyBase = up.URL
	t.Cleanup(func() { WorkBuddyBase = old })
	l := NewLane()
	l.SetWorkBuddyAccount(WBAccount{UID: "one", AccessToken: "token", Enabled: true})
	l.catalog = []ModelInfo{
		{ID: "deepseek-v4.1-flash", Channel: ChannelWorkBuddy, Pricing: modelprice.Info{Rate: modelprice.Number(.5)}},
		{ID: "unknown", Channel: ChannelWorkBuddy},
	}
	l.ProbeRound(context.Background(), false)
	if ran, err := l.WorkBuddyDailyActivity(context.Background()); ran || err != nil {
		t.Fatalf("paid daily activity ran: %v %v", ran, err)
	}
	if got := l.nextCandidateWhere(Needs{}, map[string]bool{}, true); got != "" {
		t.Fatalf("background failover selected %s", got)
	}
	if calls.Load() != 0 {
		t.Fatalf("background spent %d paid requests", calls.Load())
	}
}

func TestExplicitChannelPinsIdenticalModelIDs(t *testing.T) {
	var wbCalls, zenCalls atomic.Int32
	wb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { wbCalls.Add(1); w.WriteHeader(503) }))
	defer wb.Close()
	zen := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { zenCalls.Add(1); w.WriteHeader(503) }))
	defer zen.Close()
	oldWB, oldZen := WorkBuddyBase, UpstreamBase
	WorkBuddyBase, UpstreamBase = wb.URL, zen.URL
	t.Cleanup(func() { WorkBuddyBase, UpstreamBase = oldWB, oldZen })
	l := NewLane()
	l.SetWorkBuddyAccount(WBAccount{UID: "one", AccessToken: "token", Enabled: true})
	l.catalog = []ModelInfo{{ID: "same", Channel: "", Wire: "chat"}, {ID: "same", Channel: ChannelWorkBuddy, Wire: "chat"}}
	_, _, _ = l.attemptModel(context.Background(), Request{Channel: ChannelWorkBuddy}, "same", "", func(Chunk) {})
	if wbCalls.Load() == 0 || zenCalls.Load() != 0 {
		t.Fatalf("wrong source: wb=%d zen=%d", wbCalls.Load(), zenCalls.Load())
	}
}
