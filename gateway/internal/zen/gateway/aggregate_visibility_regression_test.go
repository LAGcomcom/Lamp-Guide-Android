package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zenui/internal/zen/agents"
	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

func TestAggregateExcludedModelsNeverReturnThroughRawOrFallbackLists(t *testing.T) {
	for _, accounts := range []int{0, 1} {
		t.Run(string(rune('0'+accounts)), func(t *testing.T) {
			up := fakeUpstream(t, []string{"[DONE]"})
			defer up.Close()
			s := newTestServer(t, up)
			aggHooks(t, func(bool) map[string]lane.QuotaExpiry {
				return map[string]lane.QuotaExpiry{"opencode": {Never: true}, "kilo": {Never: true}}
			}, func(string) int { return accounts })
			var excluded []string
			for channel, models := range s.aggCatalogs() {
				for _, m := range models {
					excluded = append(excluded, channel+"|"+m.ID)
				}
			}
			s.Store.Mutate(func(c *store.Config) {
				c.AggregateEnabled = true
				c.AggregateExclude = excluded
			})
			got := s.InjectableModels()
			if len(got) != 1 || got[0].ID != "aggregate/auto" {
				t.Fatalf("excluded models reappeared in injection: %+v", got)
			}
			cards := s.aggregateModelCards(s.Store.Config())
			if len(cards) != 1 || cards[0]["id"] != "aggregate/auto" {
				t.Fatalf("API roster disagrees with injection: %+v", cards)
			}
			if len(s.aggregateChain("aggregate/auto")) != 0 {
				t.Fatal("excluded models must not be routed through auto")
			}
		})
	}
}

func TestAggregateExclusionImmediatelyUpdatesZCodeConfig(t *testing.T) {
	up := fakeUpstream(t, []string{"[DONE]"})
	defer up.Close()
	s := newTestServer(t, up)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, ".zcode", "v2", "provider_config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = []store.Provider{{ID: "review", Enabled: true, Models: []string{"Qwen-3.8-Flash", "qwen3.8-flash"}}}
	})
	aggHooks(t, func(bool) map[string]lane.QuotaExpiry {
		return map[string]lane.QuotaExpiry{"review": {Never: true}}
	}, func(string) int { return 1 })
	reg := agents.NewRegistry(s.Store)
	s.SetAgents(reg)
	refresh := func() { reg.SetEndpoints("http://127.0.0.1:8787/v1", s.InjectableModels()) }
	s.SetModelsSync(refresh)
	refresh()
	if err := reg.Enable("zcode"); err != nil {
		t.Fatal(err)
	}
	readIDs := func() []string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Config struct {
				Providers struct {
					Rules []struct {
						Name   string `json:"providerName"`
						Config struct {
							IDs []string `json:"personalModelIds"`
						} `json:"config"`
					} `json:"providerRules"`
				} `json:"providerConfigRules"`
			} `json:"config"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		for _, rule := range doc.Config.Providers.Rules {
			if rule.Name == "Zen Gate" {
				return rule.Config.IDs
			}
		}
		t.Fatal("Zen Gate provider missing")
		return nil
	}
	assertCount := func(want int) {
		t.Helper()
		count := 0
		for _, id := range readIDs() {
			if !strings.HasPrefix(id, aggregatePrefix) {
				t.Fatalf("raw model escaped aggregation: %s", id)
			}
			if id == "aggregate/qwen3-8-flash" {
				count++
			}
		}
		if count != want {
			t.Fatalf("ZCode Qwen count = %d, want %d", count, want)
		}
	}
	assertCount(1)
	// One disabled spelling leaves the other candidate available.
	aggPost(t, s, "/admin/api/aggregate/settings", `{"excluded":["review|Qwen-3.8-Flash"]}`)
	assertCount(1)
	// Disabling both must synchronously remove the model, without a probe or restart.
	aggPost(t, s, "/admin/api/aggregate/settings", `{"excluded":["review|Qwen-3.8-Flash","review|qwen3.8-flash"]}`)
	assertCount(0)
	aggPost(t, s, "/admin/api/aggregate/settings", `{"excluded":[]}`)
	assertCount(1)
	// The state must carry exclusions even when that candidate temporarily
	// disappears from the catalog, so editing another row does not erase it.
	out := aggPost(t, s, "/admin/api/aggregate/settings", `{"excluded":["review|temporarily-unlisted-model"]}`)
	state, _ := out["state"].(map[string]any)
	excluded, _ := state["excluded"].([]any)
	if len(excluded) != 1 || excluded[0] != "review|temporarily-unlisted-model" {
		t.Fatalf("saved exclusion missing from round-trip state: %v", excluded)
	}
}

func TestAggregateOldIDsAndVisibilityRemainCompatible(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{"[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = []store.Provider{{ID: "review", Enabled: true, Models: []string{"qwen3.8-flash"}}}
		c.HiddenModels = []string{"aggregate/qwen-3-8-flash", "aggregate/auto"}
	})
	aggHooks(t, func(bool) map[string]lane.QuotaExpiry {
		return map[string]lane.QuotaExpiry{"review": {Never: true}}
	}, func(string) int { return 1 })
	if len(s.aggregateChain("aggregate/qwen-3-8-flash")) != 1 {
		t.Fatal("old aggregate id must still route to its original model")
	}
	for _, m := range s.InjectableModels() {
		if m.ID == "aggregate/qwen3-8-flash" || m.ID == "aggregate/auto" {
			t.Fatalf("hidden aggregate model is still advertised: %s", m.ID)
		}
	}
	if pos, ok := aggregateManualPosition(map[string][]string{"qwen-3-8-flash": {"review"}}, "qwen3-8-flash", "review"); !ok || pos != 0 {
		t.Fatal("old manual ranking key lost after normalization")
	}
}
