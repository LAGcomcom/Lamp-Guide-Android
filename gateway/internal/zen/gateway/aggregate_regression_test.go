package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

func TestRegressionAggregateMustPreserveModel(t *testing.T) {
	var mu sync.Mutex
	var actual []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mid, _ := body["model"].(string)
		mu.Lock()
		actual = append(actual, mid)
		mu.Unlock()
		if mid == "deepseek-v4.1-flash" {
			w.WriteHeader(429)
			fmt.Fprint(w, "{\"code\":6004,\"msg\":\"quota exhausted\"}")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"different model answered\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer up.Close()
	oldWB := lane.WorkBuddyBase
	lane.WorkBuddyBase = up.URL
	defer func() { lane.WorkBuddyBase = oldWB }()
	s := newTestServer(t, up)
	s.Lane.SetWorkBuddyAccount(lane.WBAccount{AccessToken: "review-dummy", UID: "review-user", ExpiresAtMS: time.Now().Add(48 * time.Hour).UnixMilli(), Enabled: true})
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	s.Lane.RefreshCatalog(stopped)
	var backupHits atomic.Int32
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { backupHits.Add(1); http.Error(w, "backup", 503) }))
	defer backup.Close()
	chain := []aggTarget{
		{channel: "workbuddy", realID: "deepseek-v4.1-flash", laneModel: "deepseek-v4.1-flash"},
		{channel: "qoder", realID: "deepseek-same", model: "deepseek-same", provider: &store.Provider{ID: "review-backup", BaseURL: backup.URL, Protocol: store.ProtocolOpenAI}},
	}
	out, ue := s.aggregateTurn(context.Background(), lane.Request{Model: "aggregate/deepseek", Messages: []lane.Message{{Role: lane.RoleUser, Parts: []lane.Part{lane.TextPart{Text: "hi"}}}}}, chain, func(lane.Chunk) {})
	mu.Lock()
	defer mu.Unlock()
	if len(actual) > 1 && actual[1] != "deepseek-v4.1-flash" {
		t.Fatalf("same-model aggregate escaped to another model: upstream=%v; reported ServedModel=%q; backup hits=%d; error=%v", actual, out.ServedModel, backupHits.Load(), ue)
	}
}
func TestRegressionAggregateCatalogIncludesCustomProviders(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{"[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.Providers = []store.Provider{{ID: "review-custom", Enabled: true, Models: []string{"deepseek-v4.1-flash"}}}
	})
	catalogs := s.aggCatalogs()
	if len(catalogs["review-custom"]) == 0 {
		t.Fatal("enabled custom provider is absent from aggregate catalog")
	}
}
func TestRegressionAggregateQoderTargetsIndependent(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{"[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = []store.Provider{{ID: qoderProviderID, Enabled: true, BaseURL: "http://127.0.0.1:8095/v1", Models: []string{"gfmodel"}, ModelMeta: map[string]store.ModelMeta{"gfmodel": {Name: "GLM-5.3-Flash"}}}}
	})
	aggHooks(t, func(bool) map[string]lane.QuotaExpiry {
		return map[string]lane.QuotaExpiry{"qoder": {Never: true}, "qodercn": {Never: true}}
	}, func(string) int { return 1 })
	chain := s.aggregateChain("aggregate/glm-5-3-flash")
	if len(chain) != 1 {
		t.Fatalf("shared Qoder forwarder must appear once, got %d", len(chain))
	}
}
func TestRegressionAggregateHonorsRealExpiry(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{"[DONE]"}))
	s.Lane.SetWorkBuddyAccount(lane.WBAccount{AccessToken: "review-dummy", UID: "review-user", ExpiresAtMS: time.Now().Add(48 * time.Hour).UnixMilli(), Enabled: true})
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	s.Lane.RefreshCatalog(stopped)
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.WorkBuddy.Enabled = true // Match the enabled live account installed above.
		c.Providers = []store.Provider{{ID: qoderProviderID, Enabled: true, BaseURL: "http://127.0.0.1:8095/v1", Models: []string{"deepseek-model"}, ModelMeta: map[string]store.ModelMeta{"deepseek-model": {Name: "DeepSeek V4.1 Flash"}}}}
	})
	now := time.Now()
	aggHooks(t, func(bool) map[string]lane.QuotaExpiry {
		return map[string]lane.QuotaExpiry{"qoder": {Known: true, ExpiryMS: now.Add(72 * time.Hour).UnixMilli()}, "workbuddy": {Known: true, ExpiryMS: now.Add(time.Hour).UnixMilli()}}
	}, func(string) int { return 1 })
	chain := s.aggregateChain("aggregate/deepseek-v4-1-flash")
	if len(chain) != 2 {
		t.Fatalf("expected 2 independent targets, got %d", len(chain))
	}
	state := s.buildAggregateState(false)
	var panelFirst string
	for _, v := range state["virtual"].([]map[string]any) {
		if v["key"] == "deepseek-v4-1-flash" {
			panelFirst = v["candidates"].([]map[string]any)[0]["channel"].(string)
		}
	}
	if chain[0].channel != "workbuddy" {
		t.Fatalf("workbuddy expires in 1h, qoder in 72h: execution first=%s, panel first=%s; expected workbuddy", chain[0].channel, panelFirst)
	}
}
