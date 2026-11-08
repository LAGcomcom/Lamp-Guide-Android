package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"zenui/internal/zen/store"
)

// 自建渠道的模型也要带来源名：Qoder 的 DeepSeek-Flash 和 WorkBuddy 的同名
// 条目会在同一张表里并排出现，缺了这一列用户分不清点的是哪一条。
func TestCustomProviderRowsCarrySource(t *testing.T) {
	up := httptest.NewServer(nil)
	s := newTestServer(t, up)
	// 只写配置，不触发网络刷新：RefreshCatalog 会去打真实上游列表，
	// 单测里既没有上下文也拿不到那些端点。
	s.Store.Mutate(func(cfg *store.Config) {
		cfg.Providers = append(cfg.Providers, store.Provider{
			ID: "qoder", Name: "Qoder 账号渠道", Protocol: "openai", Enabled: true,
			Models: []string{"DeepSeek-Flash"},
		})
	})

	out := get(t, s, "/admin/api/state")
	var payload struct {
		Models []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
			Custom bool   `json:"custom"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("state 不是合法 JSON: %v", err)
	}
	found := false
	for _, m := range payload.Models {
		if m.Custom && m.ID == "qoder/DeepSeek-Flash" {
			found = true
			if m.Source != "Qoder 账号渠道" {
				t.Errorf("自建渠道行缺来源名: %+v", m)
			}
		}
	}
	if !found {
		t.Fatalf("state 里没有 qoder/DeepSeek-Flash 这一行")
	}
}

// lane 行的来源翻译（免费车道留空）。
func TestLaneSourceLabel(t *testing.T) {
	cases := map[string]string{
		"workbuddy": "WorkBuddy",
		"kilo":      "Kilo",
		"opencode":  "",
		"":          "",
		"weird":     "weird",
	}
	for in, want := range cases {
		if got := laneSourceLabel(in); got != want {
			t.Errorf("laneSourceLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
