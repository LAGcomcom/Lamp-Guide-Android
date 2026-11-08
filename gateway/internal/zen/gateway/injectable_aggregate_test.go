package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

// 聚合开着时，注入清单只剩聚合规范名：一个模型的多条退路已经被折算成
// 一个名字，再把供应商的几十个真实 id 铺进选择器，等于让用户手工做一遍
// 聚合本来要做的事。
func TestInjectableModelsAggregateOnly(t *testing.T) {
	up := fakeUpstream(t, []string{`{"choices":[{"delta":{"content":"x"}}]}`, "data: [DONE]"})
	defer up.Close()
	s := newTestServer(t, up)

	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = append(c.Providers, store.Provider{
			ID: qoderProviderID, Name: "Qoder 账号渠道", Protocol: store.ProtocolOpenAI,
			Enabled: true, Models: []string{"gfmodel"},
			ModelMeta: map[string]store.ModelMeta{"gfmodel": {Name: "GLM-5.3-Flash"}},
			BaseURL:   qoderGatewayURL + "/v1",
		})
	})
	aggHooks(t,
		func(bool) map[string]lane.QuotaExpiry {
			return map[string]lane.QuotaExpiry{
				"qoder":   {Known: true, ExpiryMS: time.Now().Add(time.Hour).UnixMilli()},
				"qodercn": {Known: true, ExpiryMS: time.Now().Add(72 * time.Hour).UnixMilli()},
			}
		},
		func(string) int { return 1 })

	for _, m := range s.InjectableModels() {
		/* 只判这一条具体的供应商 id：免费车道的目录里也存在带 "/" 的名字
		   （stealth/crouton-alpha 这种），按"有没有斜杠"判会把它们误判成供应商模型。 */
		if m.ID == qoderProviderID+"/gfmodel" {
			t.Fatal("聚合开着时不应注入供应商真实 id")
		}
	}
	found := false
	for _, m := range s.InjectableModels() {
		if m.ID == aggregatePrefix+"auto" {
			found = true
		}
	}
	if !found {
		t.Fatal("聚合开着时 auto 必须在列：它是这一层的入口")
	}
}

// 关掉聚合之后，注入清单回到"每条真实 id 各自一个模型"。
func TestInjectableModelsPerProviderWhenAggregateOff(t *testing.T) {
	up := fakeUpstream(t, []string{`{"choices":[{"delta":{"content":"x"}}]}`, "data: [DONE]"})
	defer up.Close()
	s := newTestServer(t, up)

	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = false
		c.Providers = append(c.Providers, store.Provider{
			ID: "nvidia-nim", Name: "NVIDIA NIM", Protocol: store.ProtocolOpenAI,
			Enabled: true, Models: []string{"deepseek-ai/deepseek-v4.1-flash"},
			BaseURL: "https://integrate.api.nvidia.com/v1",
		})
	})
	found := false
	for _, m := range s.InjectableModels() {
		if m.ID == "nvidia-nim/deepseek-ai/deepseek-v4.1-flash" {
			found = true
		}
	}
	if !found {
		t.Fatal("聚合关掉后应逐家列出供应商模型")
	}
}

// /v1/models 与注入清单必须说同一件事：聚合开着时只广播聚合名。
// 只在其中一处生效，会让"我用的是哪个模型"随客户端而变。
func TestModelsEndpointHidesProviderIdsWhenAggregateOn(t *testing.T) {
	up := fakeUpstream(t, []string{`{"choices":[{"delta":{"content":"x"}}]}`, "data: [DONE]"})
	defer up.Close()
	s := newTestServer(t, up)

	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = append(c.Providers, store.Provider{
			ID: "nvidia-nim", Name: "NVIDIA NIM", Protocol: store.ProtocolOpenAI,
			Enabled: true, Models: []string{"deepseek-ai/deepseek-v4.1-flash"},
			BaseURL: "https://integrate.api.nvidia.com/v1",
		})
	})

	rec := httptest.NewRecorder()
	s.handleListModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != 200 {
		t.Fatalf("models status = %d", rec.Code)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, m := range out.Data {
		if m.ID == "nvidia-nim/deepseek-ai/deepseek-v4.1-flash" {
			t.Fatal("聚合开着时 /v1/models 不应广播供应商真实 id")
		}
	}
}
