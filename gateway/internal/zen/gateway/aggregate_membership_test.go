package gateway

import (
	"testing"

	"zenui/internal/zen/chprov"
	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

func TestAggregateChannelMembershipIsIndependentAndPersisted(t *testing.T) {
	up := fakeUpstream(t, []string{"[DONE]"})
	defer up.Close()
	s := newTestServer(t, up)
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.AggregateExclude = []string{"review-a|blocked-model"}
		c.Providers = []store.Provider{
			{ID: "review-a", Enabled: true, Models: []string{"same-model", "blocked-model"}},
			{ID: "review-b", Enabled: true, Models: []string{"same-model"}},
		}
	})
	aggHooks(t, func(bool) map[string]lane.QuotaExpiry {
		return map[string]lane.QuotaExpiry{"review-a": {Never: true}, "review-b": {Never: true}}
	}, func(string) int { return 1 })
	if len(s.aggregateChain("aggregate/same-model")) != 2 {
		t.Fatal("new channels must participate by default")
	}
	aggPost(t, s, "/admin/api/aggregate/settings", `{"channelEnabled":{"review-a":false}}`)
	chain := s.aggregateChain("aggregate/same-model")
	if len(chain) != 1 || chain[0].channel != "review-b" {
		t.Fatalf("disabling one channel changed the other: %+v", chain)
	}
	if !s.Store.Config().Providers[0].Enabled {
		t.Fatal("aggregate membership must not disable the direct provider")
	}
	held := s.Store.Config()
	aggPost(t, s, "/admin/api/aggregate/settings", `{"channelEnabled":{"review-b":false}}`)
	if len(held.AggregateChannels) != 1 {
		t.Fatal("membership mutation changed a previously published config snapshot")
	}
	for _, m := range s.InjectableModels() {
		if m.ID == "aggregate/same-model" {
			t.Fatal("all-disabled model leaked into injection")
		}
	}
	reopened, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Config().AggregateChannels) != 2 || reopened.Config().AggregateChannels["review-a"] || reopened.Config().AggregateChannels["review-b"] {
		t.Fatal("channel membership did not persist")
	}
	aggPost(t, s, "/admin/api/aggregate/settings", `{"channelEnabled":{"review-a":true}}`)
	chain = s.aggregateChain("aggregate/same-model")
	if len(chain) != 1 || chain[0].channel != "review-a" {
		t.Fatalf("re-enabling a channel changed another channel: %+v", chain)
	}
	if len(s.aggregateChain("aggregate/blocked-model")) != 0 {
		t.Fatal("re-enabling a channel erased a per-model exclusion")
	}
	// Even when every account is unavailable, the fallback cannot restore
	// models belonging only to a channel the user disconnected.
	aggTestAccounts = func(string) int { return 0 }
	aggPost(t, s, "/admin/api/aggregate/settings", `{"channelEnabled":{"review-a":false}}`)
	for _, m := range s.InjectableModels() {
		if m.ID == "aggregate/same-model" {
			t.Fatal("fallback resurrected a disconnected channel")
		}
	}
}

func TestAggregateWorkBuddySwitchLoadsModelsWithoutLosingLogin(t *testing.T) {
	up := fakeUpstream(t, []string{"[DONE]"})
	defer up.Close()
	s := newTestServer(t, up)
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.WorkBuddy.Enabled = false
		c.ChannelAccounts = map[string]chprov.Accounts{"workbuddy": {{
			ID: "review-wb", AccessToken: "review-dummy", Source: "poll",
		}}}
		c.AggregateExclude = []string{"workbuddy|hy3"}
	})
	aggHooks(t, func(bool) map[string]lane.QuotaExpiry {
		return map[string]lane.QuotaExpiry{"workbuddy": {Never: true}}
	}, nil)
	refreshes := 0
	s.SetModelsSync(func() { refreshes++ })
	aggPost(t, s, "/admin/api/aggregate/settings", `{"channelEnabled":{"workbuddy":true}}`)
	if refreshes != 1 {
		t.Fatal("membership change did not refresh client models")
	}
	if !s.Store.Config().WorkBuddy.Enabled || len(wbRoster(t, s.Lane)) != 3 {
		t.Fatal("enabling WorkBuddy participation did not synchronously load its account models")
	}
	if len(s.aggregateChain("aggregate/deepseek-v4-1-flash")) != 1 || len(s.aggregateChain("aggregate/hy3")) != 0 {
		t.Fatal("enabled WorkBuddy must participate while preserving per-model exclusions")
	}
	out := aggPost(t, s, "/admin/api/aggregate/settings", `{"channelEnabled":{"workbuddy":false}}`)
	if len(s.aggregateChain("aggregate/deepseek-v4-1-flash")) != 0 {
		t.Fatal("disconnected WorkBuddy still participates in aggregation")
	}
	if len(wbRoster(t, s.Lane)) != 3 || s.Lane.WorkBuddySnapshot().AccessToken != "review-dummy" {
		t.Fatal("leaving aggregation must preserve login and direct models")
	}
	state := out["state"].(map[string]any)
	for _, raw := range state["channels"].([]any) {
		c := raw.(map[string]any)
		if c["id"] == "workbuddy" && c["aggregateEnabled"] != false {
			t.Fatal("UI membership state disagrees with routing")
		}
	}
	aggPost(t, s, "/admin/api/aggregate/settings", `{"channelEnabled":{"workbuddy":true}}`)
	if len(s.aggregateChain("aggregate/deepseek-v4-1-flash")) != 1 {
		t.Fatal("reconnecting WorkBuddy did not restore its original model")
	}
}
