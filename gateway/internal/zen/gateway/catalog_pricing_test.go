package gateway

import (
	"encoding/json"
	"testing"

	"zenui/internal/zen/lane"
	"zenui/internal/zen/modelprice"
	"zenui/internal/zen/store"
)

func TestAggregateUsesCompletePaidCatalogAndRetainsPerCandidateRates(t *testing.T) {
	up := fakeUpstream(t, []string{"[DONE]"})
	defer up.Close()
	s := newTestServer(t, up)
	p := store.Provider{ID: "catalog-test", Name: "Catalog", Enabled: true, BaseURL: up.URL, Models: []string{"free-direct"}}
	mergeCatalog(&p, []lane.ListingMeta{
		{ID: "free-direct", Pricing: modelprice.Info{Rate: modelprice.Number(0)}},
		{ID: "paid-chat", Name: "Paid Chat", Pricing: modelprice.Info{Rate: modelprice.Number(1.62), Promotions: []modelprice.Promotion{{Rate: .5}}}},
		{ID: "unknown-chat"},
	})
	s.Store.Mutate(func(c *store.Config) { c.AggregateEnabled = true; c.Providers = []store.Provider{p} })
	aggHooks(t, func(bool) map[string]lane.QuotaExpiry {
		return map[string]lane.QuotaExpiry{"catalog-test": {Never: true}}
	}, func(string) int { return 1 })
	chain := s.aggregateChain("aggregate/paid-chat")
	if len(chain) != 1 || chain[0].model != "paid-chat" {
		t.Fatalf("paid catalog-only model cannot route: %+v", chain)
	}
	if got := s.Store.Config().Providers[0].Models; len(got) != 1 || got[0] != "free-direct" {
		t.Fatalf("direct selection changed: %v", got)
	}
	encoded, err := json.Marshal(s.buildAggregateState(false))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Virtual []struct {
			Candidates []struct {
				Channel string   `json:"channel"`
				ID      string   `json:"realId"`
				Price   *float64 `json:"price"`
				Rate    *float64 `json:"creditsRate"`
			} `json:"candidates"`
		} `json:"virtual"`
	}
	if err = json.Unmarshal(encoded, &state); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, vm := range state.Virtual {
		for _, c := range vm.Candidates {
			if c.Channel != "catalog-test" {
				continue
			}
			seen[c.ID] = true
			switch c.ID {
			case "paid-chat":
				if c.Price == nil || *c.Price != .5 || c.Rate == nil || *c.Rate != 1.62 {
					t.Errorf("discount/base missing: %+v", c)
				}
			case "free-direct":
				if c.Price == nil || *c.Price != 0 {
					t.Errorf("zero became unknown: %+v", c)
				}
			case "unknown-chat":
				if c.Price != nil || c.Rate != nil {
					t.Errorf("unknown became free: %+v", c)
				}
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("full catalog missing candidates: %v", seen)
	}
	s.Store.Mutate(func(c *store.Config) { c.AggregateExclude = []string{"catalog-test|paid-chat"} })
	if len(s.aggregateChain("aggregate/paid-chat")) != 0 {
		t.Fatal("full-catalog discovery resurrected excluded paid model")
	}
	for _, m := range s.InjectableModels() {
		if m.ID == "aggregate/paid-chat" {
			t.Fatal("disabled paid model returned to client roster")
		}
	}
}

func TestQoderWrappedListingPreservesRatesAndCapabilities(t *testing.T) {
	rows := parseQoderCatalog([]byte(`{"data":[{"id":"paid","name":"GPT-6","is_vl":true,"is_reasoning":true,"price_factor":1.5},{"id":"free","price_factor":0,"original_price_factor":0.1},{"id":"unknown","pricing":{"input":2}}]}`))
	if len(rows) != 3 {
		t.Fatalf("Qoder catalog = %+v", rows)
	}
	if !rows[0].Vision || !rows[0].Reasoning || !rows[0].InputDeclared || rows[0].Pricing.Rate == nil || *rows[0].Pricing.Rate != 1.5 {
		t.Fatalf("Qoder metadata lost: %+v", rows[0])
	}
	if rows[1].Pricing.Rate == nil || *rows[1].Pricing.Rate != .1 || len(rows[1].Pricing.Promotions) != 1 || rows[1].Pricing.Promotions[0].Rate != 0 {
		t.Fatalf("free/original price lost: %+v", rows[1])
	}
	if rows[2].Pricing.Rate != nil {
		t.Fatal("Qoder token cost misrepresented as credit multiplier")
	}
	p := store.Provider{Models: []string{"paid"}}
	mergeCatalog(&p, rows)
	if len(p.CatalogModels) != 3 || p.ModelMeta["free"].Pricing.Rate == nil {
		t.Fatal("relay dropped non-selected model pricing")
	}
}
