package gateway

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"zenui/internal/zen/lane"
	"zenui/internal/zen/relay"
	"zenui/internal/zen/store"
)

type CatalogRefreshReport struct {
	Updated []string `json:"updated"`
	Failed  []string `json:"failed"`
}

// RefreshProviderCatalogs discovers all available models without changing the
// direct-model selection, login state, channel membership or exclusions.
func (s *Server) RefreshProviderCatalogs(ctx context.Context) (CatalogRefreshReport, error) {
	type result struct {
		provider store.Provider
		rows     []lane.ListingMeta
		err      error
	}
	providers := s.Store.Config().Providers
	results := make(chan result, len(providers))
	slots := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		wg.Add(1)
		go func(p store.Provider) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				results <- result{provider: p, err: ctx.Err()}
				return
			}
			defer func() { <-slots }()
			var rows []lane.ListingMeta
			var err error
			if p.ID == qoderProviderID {
				var status int
				var body []byte
				status, body, err = qoderGet("/v1/models")
				if err == nil && status == 200 {
					rows = parseQoderCatalog(body)
				}
				if len(rows) == 0 && err == nil {
					err = errors.New("empty Qoder catalog")
				}
			} else {
				rows, err = relay.FetchModelCatalog(ctx, p.BaseURL, p.APIKey, p.Protocol)
			}
			results <- result{p, rows, err}
		}(p)
	}
	wg.Wait()
	close(results)
	report := CatalogRefreshReport{Updated: []string{}, Failed: []string{}}
	var successful []result
	for r := range results {
		if r.err != nil {
			report.Failed = append(report.Failed, r.provider.Name)
		} else {
			successful = append(successful, r)
		}
	}
	if len(successful) == 0 {
		return report, nil
	}
	s.Store.Mutate(func(cfg *store.Config) {
		for _, r := range successful {
			for i := range cfg.Providers {
				p := &cfg.Providers[i]
				// A slow listing cannot write metadata into a provider edited/deleted
				// or disabled while the request was running.
				if p.ID != r.provider.ID || !p.Enabled || p.BaseURL != r.provider.BaseURL || p.APIKey != r.provider.APIKey || p.Protocol != r.provider.Protocol {
					continue
				}
				mergeCatalog(p, r.rows)
				report.Updated = append(report.Updated, p.Name)
				break
			}
		}
	})
	if len(report.Updated) > 0 {
		if err := s.Store.Save(); err != nil {
			return report, err
		}
		if s.modelsSync != nil {
			s.modelsSync()
		}
	}
	sort.Strings(report.Updated)
	sort.Strings(report.Failed)
	return report, nil
}

func (s *Server) adminAggregateRefreshModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	var report CatalogRefreshReport
	var providerErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); report, providerErr = s.RefreshProviderCatalogs(ctx) }()
	nativeReport := s.refreshNativeCatalogs(ctx)
	s.Lane.RefreshCatalogNow(ctx)
	wg.Wait()
	report.Updated = append(report.Updated, nativeReport.Updated...)
	report.Failed = append(report.Failed, nativeReport.Failed...)
	if providerErr != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "模型目录保存失败"})
		return
	}
	if s.modelsSync != nil {
		s.modelsSync()
	}
	writeJSON(w, 200, map[string]any{"ok": true, "report": report, "state": s.buildAggregateState(false)})
}
