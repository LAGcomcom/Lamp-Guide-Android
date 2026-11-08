package chprov

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"zenui/internal/zen/modelprice"
)

var NativeChannelIDs = []string{"astudio", "autoclaw", "autoclawintl", "magicrew", "magicrewintl"}

func IsNativeChannel(id string) bool {
	for _, v := range NativeChannelIDs {
		if v == id {
			return true
		}
	}
	return false
}

type NativeModel struct {
	ID, Name                 string
	ContextWindow, MaxOutput int
	Vision                   bool
	Pricing                  modelprice.Info
}

// NativeCatalog reads the complete live directory, including points-billed
// entries. Unknown prices remain unknown; no compiled fallback pretends to be
// an account's authoritative catalog.
func NativeCatalog(ctx context.Context, id string, a Account) ([]NativeModel, error) {
	if !IsNativeChannel(id) || a.Empty() {
		return nil, errors.New("渠道不支持或未登录")
	}
	s := MustSpec(id).Quota
	h := rewardHeaders(id, a)
	switch id {
	case "astudio":
		s.BaseURL, s.Path = "https://astronstudio-api-volces-prod.xf-yun.com", "/api/v1/model-manager/models"
		h = NativeChatHeaders(id, a, "")
	case "autoclaw", "autoclawintl":
		s.Path = "/autoclaw-proxy/proxy/autoclaw-model-config"
		delete(h, "X-Authorization")
		h["X-Client-Type"] = "pc"
	default:
		if id == "magicrewintl" && a.Field("organization_code") == "" {
			return nil, errors.New("请先选择国际站组织并导入组织码")
		}
		s.Path = "/api/v1/flows/models"
	}
	p, status, failure := checkinJSONRequest(ctx, "GET", s.BaseURL+s.Path, h, "", strings.HasPrefix(id, "autoclaw"))
	if failure != nil {
		return nil, errors.New("模型目录请求失败，请检查网络和登录状态")
	}
	if status != 200 {
		return nil, errors.New("模型目录返回非成功状态")
	}
	// AutoClaw's model config can be a bare object without a business envelope.
	if !strings.HasPrefix(id, "autoclaw") || p["code"] != nil {
		if failure = rewardEnvelope(id, p, status); failure != nil {
			return nil, errors.New(failure.Message)
		}
	}
	rows := nativeModelRows(p, 0)
	if rows == nil {
		return nil, errors.New("模型目录缺少有效列表")
	}
	models, seen := []NativeModel{}, map[string]bool{}
	for _, row := range rows {
		r, ok := row.(map[string]any)
		if !ok {
			continue
		}
		mid := firstNonEmpty(field(r, "id"), field(r, "model_id"), field(r, "modelId"))
		if strings.HasPrefix(id, "autoclaw") {
			mid = firstNonEmpty(mid, field(r, "model"), field(r, "name"), field(r, "code"))
		}
		name := firstNonEmpty(field(r, "name"), field(r, "display_name"), mid)
		switch id {
		case "astudio":
			mid = field(r, "slug")
			if strings.HasPrefix(mid, "builtin/") {
				raw := strings.TrimPrefix(mid, "builtin/")
				b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(raw, "="))
				if err == nil && len(b) > 0 && !strings.ContainsAny(string(b), "/\x00") {
					mid = string(b)
				}
			}
			if field(r, "visibility") == "hide" {
				continue
			}
			name = firstNonEmpty(field(r, "display_name"), mid)
		case "magicrew", "magicrewintl":
			mid, name = field(r, "value"), firstNonEmpty(field(r, "label"), field(r, "value"))
		}
		if mid == "" || seen[mid] {
			continue
		}
		seen[mid] = true
		m := NativeModel{ID: mid, Name: name, Pricing: modelprice.Parse(r)}
		m.ContextWindow = nativePositiveInt(r, "contextWindow", "context_window", "context_length", "maxContext", "max_context", "max_input_tokens", "maxInputTokens")
		m.MaxOutput = nativePositiveInt(r, "maxTokens", "max_output_tokens", "max_tokens", "maxOutputTokens", "max_output")
		m.Vision = r["vision"] == true || r["supportsImage"] == true || r["supports_image"] == true || r["support_image"] == true
		for _, key := range []string{"input", "input_modalities", "inputModalities", "modalities"} {
			if list, ok := r[key].([]any); ok {
				for _, v := range list {
					if text, ok := v.(string); ok && strings.EqualFold(text, "image") {
						m.Vision = true
					}
				}
			}
		}
		models = append(models, m)
	}
	if len(models) == 0 && len(rows) > 0 {
		return nil, errors.New("模型目录条目无法识别")
	}
	if strings.HasPrefix(id, "magicrew") && len(models) > 0 {
		ids := make([]string, 0, len(models))
		for _, m := range models {
			ids = append(ids, m.ID)
		}
		b, _ := json.Marshal(map[string]any{"model_ids": ids})
		prices, status, failure := checkinRequest(ctx, "POST", s.BaseURL+"/api/v1/products/model-points", h, string(b))
		if failure == nil && rewardEnvelope(id, prices, status) == nil {
			rows, _ := prices["data"].([]any)
			if row, ok := prices["data"].(map[string]any); ok {
				rows = []any{row}
			}
			selected := map[string]map[string]any{}
			for _, row := range rows {
				r, _ := row.(map[string]any)
				mid := field(r, "model_id")
				old := selected[mid]
				if old == nil || r["billing_type"] == "TextTokens" && old["billing_type"] != "TextTokens" {
					selected[mid] = r
				}
			}
			for i := range models {
				if rate, ok := numeric(selected[models[i].ID]["input_points"]); ok && rate >= 0 && !math.IsNaN(rate) && !math.IsInf(rate, 0) {
					models[i].Pricing = modelprice.Info{Rate: modelprice.Number(rate / 2000)}
				}
			}
		}
	}
	return models, nil
}

func nativePositiveInt(row map[string]any, keys ...string) int {
	for _, k := range keys {
		if n, ok := numeric(row[k]); ok && n > 0 && n <= 1e7 {
			return int(n)
		}
	}
	return 0
}

func nativeModelRows(v any, depth int) []any {
	if depth > 4 {
		return nil
	}
	if rows, ok := v.([]any); ok {
		return rows
	}
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"models", "data", "model_list", "modelList", "list", "result"} {
			if rows := nativeModelRows(m[k], depth+1); rows != nil {
				return rows
			}
		}
	}
	return nil
}

func NativeChatHeaders(id string, a Account, model string) map[string]string {
	h := rewardHeaders(id, a)
	delete(h, "Cookie") // AStudio management Cookie must never reach the inference host.
	if id == "astudio" {
		h["Authorization"] = "Bearer " + a.AccessToken
		if account := a.Field("account_id"); account != "" {
			h["X-Account-Id"] = account
		}
	}
	if strings.HasPrefix(id, "autoclaw") {
		delete(h, "Authorization")
		h["X-Version"], h["X-Product"], h["X-Request-Model"] = "2.1.1", "autoclaw", model
	}
	if strings.HasPrefix(id, "magicrew") {
		h["Authorization"] = "Bearer " + a.Field("api_key")
	}
	h["Accept"] = "text/event-stream"
	return h
}

// PrepareNativeAccount renews AutoClaw access and issues a separate MagiCrew
// gateway key. The caller must atomically persist changes before inference.
func PrepareNativeAccount(ctx context.Context, id string, a Account) (Account, error) {
	fresh, failure := CheckinAccount(ctx, id, a)
	if failure != nil {
		return a, errors.New(failure.Message)
	}
	if !strings.HasPrefix(id, "magicrew") {
		return fresh, nil
	}
	if id == "magicrewintl" && fresh.Field("organization_code") == "" {
		return a, errors.New("缺少国际站组织码")
	}
	expires, _ := strconv.ParseInt(a.Field("api_key_expires_at"), 10, 64)
	if expires > 0 && expires < 1e11 {
		expires *= 1000
	}
	if a.Field("api_key") != "" && expires > time.Now().Add(time.Minute).UnixMilli() {
		return fresh, nil
	}
	s := MustSpec(id).Quota
	p, status, failure := checkinRequest(ctx, "POST", s.BaseURL+"/api/v1/model-gateway/tokens", rewardHeaders(id, fresh), "{}")
	if failure != nil {
		return a, errors.New(failure.Message)
	}
	if failure = rewardEnvelope(id, p, status); failure != nil {
		return a, errors.New(failure.Message)
	}
	d := objField(p, "data")
	if field(d, "api_key") == "" {
		return a, errors.New("上游未签发推理密钥")
	}
	fresh.Extra = cloneAccountExtra(fresh.Extra)
	fresh.Extra["api_key"], fresh.Extra["api_key_expires_at"] = field(d, "api_key"), field(d, "api_key_expires_at")
	return fresh, nil
}

func cloneAccountExtra(extra map[string]string) map[string]string {
	out := make(map[string]string, len(extra)+2)
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func refreshAutoclaw(ctx context.Context, id string, a Account) (Account, error) {
	if a.RefreshTok == "" {
		return a, errors.New("请重新登录")
	}
	s := MustSpec(id).Quota
	b, _ := json.Marshal(map[string]string{"refresh_token": cleanRewardToken(a.RefreshTok), "source_id": "autoclaw", "device_id": rewardDevice(a)})
	for _, path := range []string{"/userapi/v1/refresh", "/userapi/v1/agent-refresh"} {
		h := rewardHeaders(id, a)
		delete(h, "Authorization")
		delete(h, "X-Authorization")
		p, status, failure := checkinRequest(ctx, "POST", s.BaseURL+path, h, string(b))
		if failure != nil || rewardEnvelope(id, p, status) != nil {
			continue
		}
		d := objField(p, "data")
		if field(d, "access_token") == "" {
			continue
		}
		a.Extra = cloneAccountExtra(a.Extra)
		a.Extra["device_id"] = rewardDevice(a)
		a.AccessToken = cleanRewardToken(field(d, "access_token"))
		if refresh := field(d, "refresh_token"); refresh != "" {
			a.RefreshTok = cleanRewardToken(refresh)
		}
		a.ExpiresAtMS = 0
		if _, _, exp, ok := JWTClaims(a.AccessToken); ok {
			a.ExpiresAtMS = exp
		}
		if a.ExpiresAtMS == 0 {
			if n, ok := numeric(d["expires_in"]); ok && n > 0 {
				a.ExpiresAtMS = time.Now().Add(time.Duration(n) * time.Second).UnixMilli()
			}
		}
		return a, nil
	}
	return a, errors.New("AutoClaw 续期未成功，请重新导入或登录")
}
