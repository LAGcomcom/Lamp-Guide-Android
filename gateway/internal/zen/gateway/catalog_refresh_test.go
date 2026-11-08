package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

func TestRefreshProviderCatalogsPreservesSelectionsAndFailedCache(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("catalog refresh attempted %s", r.Method)
		}
		if r.URL.Path == "/good/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"free-direct","creditsRate":0},{"id":"paid-chat","creditsRate":1.8}]}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer up.Close()
	s := newTestServer(t, up)
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateChannels = map[string]bool{"good": false}
		c.AggregateExclude = []string{"good|paid-chat"}
		c.Providers = []store.Provider{
			{ID: "good", Name: "Good", Enabled: true, BaseURL: up.URL + "/good", Models: []string{"free-direct"}},
			{ID: "bad", Name: "Bad", Enabled: true, BaseURL: up.URL + "/bad", CatalogModels: []string{"cached-paid"}},
		}
	})
	synced := 0
	s.modelsSync = func() { synced++ }
	report, err := s.RefreshProviderCatalogs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Updated, []string{"Good"}) || !reflect.DeepEqual(report.Failed, []string{"Bad"}) || synced != 1 {
		t.Fatalf("report/sync = %+v, %d", report, synced)
	}
	cfg := s.Store.Config()
	p := cfg.Providers[0]
	if !reflect.DeepEqual(p.Models, []string{"free-direct"}) || !reflect.DeepEqual(p.CatalogModels, []string{"free-direct", "paid-chat"}) {
		t.Fatalf("direct selection/full catalog = %v / %v", p.Models, p.CatalogModels)
	}
	if rate := p.ModelMeta["paid-chat"].Pricing.Rate; rate == nil || *rate != 1.8 {
		t.Fatalf("paid multiplier missing: %v", rate)
	}
	if cfg.AggregateChannels["good"] || !reflect.DeepEqual(cfg.AggregateExclude, []string{"good|paid-chat"}) || !reflect.DeepEqual(cfg.Providers[1].CatalogModels, []string{"cached-paid"}) {
		t.Fatal("refresh changed exclusions/membership or lost failed source cache")
	}
}

func TestRefreshProviderCatalogsRejectsResponseAfterProviderEdit(t *testing.T) {
	for _, edit := range []string{"base", "key", "protocol", "disabled", "deleted"} {
		t.Run(edit, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-release
				_, _ = w.Write([]byte(`{"data":[{"id":"stale-response","creditsRate":2}]}`))
			}))
			defer up.Close()
			s := newTestServer(t, up)
			s.Store.Mutate(func(c *store.Config) {
				c.Providers = []store.Provider{{ID: "edit", Name: "Edit", Enabled: true, BaseURL: up.URL, APIKey: "old", Protocol: store.ProtocolOpenAI, CatalogModels: []string{"cached"}}}
			})
			done := make(chan error, 1)
			go func() {
				report, err := s.RefreshProviderCatalogs(context.Background())
				if len(report.Updated) != 0 {
					t.Error("stale response was reported as applied")
				}
				done <- err
			}()
			<-started
			s.Store.Mutate(func(c *store.Config) {
				switch edit {
				case "base":
					c.Providers[0].BaseURL += "/new"
				case "key":
					c.Providers[0].APIKey = "new"
				case "protocol":
					c.Providers[0].Protocol = store.ProtocolAnthropic
				case "disabled":
					c.Providers[0].Enabled = false
				case "deleted":
					c.Providers = nil
				}
			})
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if p := s.Store.Config().Providers; len(p) > 0 && !reflect.DeepEqual(p[0].CatalogModels, []string{"cached"}) {
				t.Fatal("old source metadata overwrote the edited provider")
			}
		})
	}
}

func TestQoderCatalogOnlyPaidModelActuallyRoutes(t *testing.T) {
	up := fakeUpstream(t, []string{"[DONE]"})
	defer up.Close()
	s := newTestServer(t, up)
	p := store.Provider{ID: qoderProviderID, Name: "Qoder", Enabled: true, BaseURL: up.URL}
	mergeCatalog(&p, parseQoderCatalog([]byte(`{"data":[{"id":"paid","name":"DeepSeek-V4-Pro","price_factor":0.5}]}`)))
	s.Store.Mutate(func(c *store.Config) { c.AggregateEnabled = true; c.Providers = []store.Provider{p} })
	aggHooks(t, func(bool) map[string]lane.QuotaExpiry {
		return map[string]lane.QuotaExpiry{"qoder": {Known: true, ExpiryMS: time.Now().Add(time.Hour).UnixMilli()}}
	}, func(string) int { return 1 })
	chain := s.aggregateChain("aggregate/deepseek-v4-pro")
	if len(chain) != 1 || chain[0].channel != "qoder" || chain[0].provider == nil || chain[0].model != "paid" {
		t.Fatalf("Qoder complete catalog has no executable target: %+v", chain)
	}
	s.Store.Mutate(func(c *store.Config) { c.AggregateChannels = map[string]bool{"qoder": false} })
	if len(s.aggregateChain("aggregate/deepseek-v4-pro")) != 0 {
		t.Fatal("catalog-only model bypassed channel off switch")
	}
}
