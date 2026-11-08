package lane

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"zenui/internal/zen/modelprice"
)

// Known callable chat IDs from the reference plugin's WorkBuddy product
// fallback. This supplies eligibility only: prices always come from upstream.
var knownWorkBuddyChatIDs = map[string]bool{
	"hy4-preview-f": true, "hy4-preview": true, "hy3": true,
	"deepseek-v4.1-flash": true, "deepseek-v4.1-flash-sg": true,
	"gpt-6-astra": true, "gpt-5.6-sol": true, "gpt-5.6-terra": true, "gpt-5.6-luna": true,
	"gpt-5.5": true, "gpt-5.4": true, "gpt-5.3-codex": true, "gemini-3.5-flash": true,
	"glm-5.3": true, "glm-5.2": true, "kimi-k3": true, "kimi-k2.8-preview": true, "kimi-k2.6": true,
}

// ParseWorkBuddyCatalog follows the reference plugin's dynamic catalog. It
// keeps chargeable chat models, excluding internal completion/image routes.
func ParseWorkBuddyCatalog(payload map[string]any) []ModelInfo {
	data, _ := payload["data"].(map[string]any)
	rows, _ := data["models"].([]any)
	referenced := map[string]bool{}
	agents, _ := data["agents"].([]any)
	for _, raw := range agents {
		a, _ := raw.(map[string]any)
		ids, _ := a["models"].([]any)
		for _, rawID := range ids {
			if id, ok := rawID.(string); ok {
				referenced[id] = true
			}
		}
	}
	features, _ := data["productFeaturesConfig"].(map[string]any)
	banner, _ := features["ModelTrialBanner"].(map[string]any)
	banners, _ := banner["banners"].([]any)
	for _, raw := range banners {
		b, _ := raw.(map[string]any)
		if id, ok := b["targetModelId"].(string); ok {
			referenced[id] = true
		}
	}
	promotions := modelprice.BuddyPromotions(data)
	known := map[string]ModelInfo{}
	for _, m := range buildWorkBuddyCatalog() {
		known[m.ID] = m
	}
	seen := map[string]bool{}
	var out []ModelInfo
	add := func(row map[string]any) {
		id, _ := row["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" || id == "auto" || id == "default" || seen[id] || strings.HasPrefix(id, "nes-") || strings.HasPrefix(id, "completion-") || strings.HasPrefix(id, "codewise-") || row["supportsExtra"] == true {
			return
		}
		if n := declaredInt(row, "maxOutputTokens"); n > 0 && n <= 256 {
			return
		}
		if tags, ok := row["tags"].([]any); ok {
			for _, tag := range tags {
				if tag == "text-to-image" {
					return
				}
			}
		}
		m, fallback := known[id]
		// Unreferenced, unrecognized backend-only rows are not callable chat
		// models. This is the same guard as the plugin's reconciliation step.
		_, imageDeclared := row["supportsImages"].(bool)
		if !knownWorkBuddyChatIDs[id] && !referenced[id] && !imageDeclared {
			return
		}
		if !fallback {
			m = ModelInfo{ID: id, Name: DisplayModelName(id), Channel: ChannelWorkBuddy, Wire: "chat", CanDisableThinking: true}
		}
		if name, ok := row["name"].(string); ok && strings.TrimSpace(name) != "" {
			m.Name = name
		}
		if n := declaredInt(row, "maxInputTokens"); n > 0 {
			m.ContextWindow = n
		}
		if n := declaredInt(row, "maxOutputTokens"); n > 0 {
			m.MaxOutput = n
		}
		if v, ok := row["supportsImages"].(bool); ok {
			m.Vision = v
		}
		if reasoning, ok := row["reasoning"].(map[string]any); ok {
			efforts, _ := reasoning["supportedEfforts"].([]any)
			m.Reasoning = len(efforts) > 0
			m.CanDisableThinking = false
			for _, e := range efforts {
				if e == "none" || e == "off" {
					m.CanDisableThinking = true
				}
			}
		}
		m.Pricing = modelprice.Parse(row)
		m.Pricing.Promotions = append(m.Pricing.Promotions, promotions[id]...)
		seen[id] = true
		out = append(out, m)
	}
	for _, raw := range rows {
		if row, ok := raw.(map[string]any); ok {
			add(row)
		}
	}
	// Agent/trial references occasionally carry an ID absent from data.models.
	for _, raw := range agents {
		a, _ := raw.(map[string]any)
		ids, _ := a["models"].([]any)
		for _, id := range ids {
			if s, ok := id.(string); ok && !seen[s] {
				add(map[string]any{"id": s})
			}
		}
	}
	for _, raw := range banners {
		b, _ := raw.(map[string]any)
		id, _ := b["targetModelId"].(string)
		if id != "" && !seen[id] {
			referenced[id] = true
			add(map[string]any{"id": id})
		}
	}
	return out
}

func fetchWBCatalogPayload(ctx context.Context, acct WBAccount, path string) map[string]any {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, WorkBuddyBase+path, nil)
	if err != nil {
		return nil
	}
	for k, v := range workBuddyHeaders(acct, wbRequestID()) {
		req.Header.Set(k, v)
	}
	req.Header.Set("X-Product", "SaaS")
	req.Header.Set("X-Product-Code", "workbuddy")
	client := &http.Client{Transport: laneHTTP.Transport, Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var payload map[string]any
	if json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload) != nil {
		return nil
	}
	if code, ok := payload["code"].(float64); ok && code != 0 {
		return nil
	}
	return payload
}

func mergeWBCatalog(primary, extra []ModelInfo, keepMissing bool) []ModelInfo {
	out := append([]ModelInfo(nil), primary...)
	idx := map[string]int{}
	for i, m := range out {
		idx[m.ID] = i
	}
	for _, m := range extra {
		if i, ok := idx[m.ID]; ok {
			// Config owns time-dependent promotions even when scoped metadata wins.
			if !keepMissing {
				out[i].Pricing.Promotions = m.Pricing.Promotions
			}
		} else {
			idx[m.ID] = len(out)
			out = append(out, m)
		}
	}
	return out
}

// Cache and single-flight are per account. A failed/partial fetch must not
// shrink a previously complete catalog down to the cold-start fallback.
func (l *Lane) fetchWorkBuddyCatalog(ctx context.Context) []ModelInfo {
	l.wbCatalogMu.Lock()
	defer l.wbCatalogMu.Unlock()
	acct, ok := l.workBuddyReady()
	if !ok {
		return nil
	}
	key := acct.UID + "|" + acct.Realm
	if key != l.wbCatalogKey {
		l.wbCatalogKey = key
		l.wbCatalogCache = nil
		l.wbCatalogAt = time.Time{}
	}
	if len(l.wbCatalogCache) > 0 && time.Since(l.wbCatalogAt) < 20*time.Minute {
		return append([]ModelInfo(nil), l.wbCatalogCache...)
	}
	scoped := fetchWBCatalogPayload(ctx, acct, "/console/enterprises/personal/models")
	config := fetchWBCatalogPayload(ctx, acct, "/v3/config")
	primary, extra := ParseWorkBuddyCatalog(scoped), ParseWorkBuddyCatalog(config)
	merged := mergeWBCatalog(primary, extra, false)
	// Attach config promotions to scoped-only IDs too.
	data, _ := config["data"].(map[string]any)
	promos := modelprice.BuddyPromotions(data)
	for i := range merged {
		if ps := promos[merged[i].ID]; len(ps) > 0 {
			merged[i].Pricing.Promotions = ps
		}
	}
	if len(primary) == 0 || len(extra) == 0 {
		merged = mergeWBCatalog(merged, l.wbCatalogCache, true)
	} else {
		l.wbCatalogAt = time.Now()
	}
	if len(merged) > 0 {
		l.wbCatalogCache = merged
	}
	if len(l.wbCatalogCache) > 0 {
		return append([]ModelInfo(nil), l.wbCatalogCache...)
	}
	return buildWorkBuddyCatalog()
}

func catalogChannel(m ModelInfo) string {
	if m.Channel == "" {
		return "opencode"
	}
	return m.Channel
}
func (m ModelInfo) BackgroundFree() bool {
	if m.Pricing.Rate != nil {
		return *m.Pricing.Rate == 0
	}
	if m.Pricing.ConfirmedFree() {
		return true
	}
	return m.Channel == "" && IsFreeLane(m.ID) || m.Channel == ChannelKilo && !m.RequiresKey
}

// ModelRequiresKey is model-specific: anonymous access does not authorize paid models.
func (l *Lane) ModelRequiresKey(channel, model string) bool {
	if channel == "opencode" {
		return !l.OpenCodeCanServe(model)
	}
	if channel != ChannelKilo {
		return false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, m := range l.catalog {
		if m.Channel == channel && m.ID == model {
			return m.RequiresKey
		}
	}
	return true
}

func (l *Lane) RefreshWorkBuddyModels(ctx context.Context) {
	acct, ready := l.workBuddyReady()
	if !ready {
		return
	}
	rows := l.fetchWorkBuddyCatalog(ctx)
	current, ready := l.workBuddyReady()
	if !ready || current.UID != acct.UID || current.Realm != acct.Realm {
		return
	}
	l.mu.Lock()
	cat := make([]ModelInfo, 0, len(l.catalog)+len(rows))
	for _, m := range l.catalog {
		if m.Channel != ChannelWorkBuddy {
			cat = append(cat, m)
		}
	}
	l.catalog = append(cat, rows...)
	l.mu.Unlock()
	if l.OnCatalogReload != nil {
		l.OnCatalogReload()
	}
	l.notify()
}
