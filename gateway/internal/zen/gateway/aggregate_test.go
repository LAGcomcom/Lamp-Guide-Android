package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"zenui/internal/zen/aggregate"
	"zenui/internal/zen/channels"
	"zenui/internal/zen/store"
)

// aggregate_test 守的是「面板读的那份契约」而不是内部实现：字段名、空表形态、
// 以及「不可用的候选照样列出来」这三条一旦破了，界面会静默少一块。
//
// ⚠️ 这里刻意不断言额度数字：本机的 workbuddy 没登录、qoder sidecar 也没起，
// 额度端点读不到是**正常状态**。断言「读不到 ⇒ 候选不可当选」才是能长期成立的
// 那条事实；断言某个到期时刻则会随账号状态翻绿翻红。

func aggGet(t *testing.T, s *Server, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1"
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET %s → %d: %s", path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("GET %s 的响应不是 JSON: %v", path, err)
	}
	return out
}

func aggPost(t *testing.T, s *Server, path, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Host = "127.0.0.1"
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("content-type", "application/json")
	req.Header.Set("origin", "http://127.0.0.1:8787")
	req.Host = "127.0.0.1:8787"
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("POST %s → %d: %s", path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("POST %s 的响应不是 JSON: %v", path, err)
	}
	return out
}

// 空目录不是错误：新装的程序没有任何渠道目录，面板要能画出「还没有候选」，
// 而不是报错或留白。契约里的四个键必须都在。
func TestAggregateStateHasContractKeysWithFreeLane(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	st := aggGet(t, s, "/admin/api/aggregate/state")
	for _, k := range []string{"enabled", "order", "channels", "virtual", "totals"} {
		if _, ok := st[k]; !ok {
			t.Fatalf("aggregate/state 缺字段 %q（前端按它渲染整面）", k)
		}
	}
	// 渠道表是编译期清单，即使没有任何目录也必须全列出来 —— 那一组还要回答
	// 「哪几家根本没资格进轮换」，空表就没有答案了。
	rows, _ := st["channels"].([]any)
	if want := len(channels.Channels()); len(rows) != want {
		t.Fatalf("channels = %d 条, 渠道表有 %d 家", len(rows), want)
	}
	totals, _ := st["totals"].(map[string]any)
	if totals == nil {
		t.Fatal("totals 不是对象")
	}
	if got := totals["wiredChannels"]; got != float64(len(channels.WiredIDs())) {
		t.Fatalf("wiredChannels = %v, want %d", got, len(channels.WiredIDs()))
	}
	if got := totals["virtualModels"].(float64); got == 0 {
		t.Fatal("已有免费车道目录，应生成虚拟模型")
	}
	// 烧掉顺序只含真有临期来源的那几家，且顺序即默认轮换序。
	order, _ := st["order"].([]any)
	want := channels.WiredIDs()
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// 读不到额度 ≠ 可用：候选照旧列出来（要能核查），但 eligible 必须为 false。
// 这条是整面的安全底线 —— 把「读不到」当成「可用」会让请求打到可能早烧光的渠道。
func TestAggregateCandidateIsNotEligibleWithoutExpiry(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	// 目录不依赖网络：qoder 的模型清单是托管供应商自己的字段，写进 store 就能用。
	// 这样这条断言在任何机器上都跑，而不是「本机没登录就跳过」。
	s.Store.Mutate(func(c *store.Config) {
		c.Providers = append(c.Providers, store.Provider{
			ID: qoderProviderID, Name: "Qoder 账号渠道", Protocol: store.ProtocolOpenAI,
			Enabled: true, Models: []string{"gfmodel"},
			ModelMeta: map[string]store.ModelMeta{"gfmodel": {Name: "GLM-5.3-Flash · x0.6"}},
			BaseURL:   qoderGatewayURL + "/v1",
		})
	})
	st := s.buildAggregateState(true)
	virtuals, _ := st["virtual"].([]map[string]any)
	if len(virtuals) == 0 {
		t.Fatal("写入了 qoder 目录却没有推导出任何虚拟模型")
	}
	var found bool
	for _, vm := range virtuals {
		if vm["key"] == "glm-5-3-flash" {
			found = true
		}
		cands, _ := vm["candidates"].([]map[string]any)
		if len(cands) == 0 {
			t.Fatalf("虚拟模型 %v 没有候选行", vm["key"])
		}
		for _, c := range cands {
			if c["channel"] != "qoder" {
				continue
			}
			// 三条契约：真实 id 保留、倍率可从展示名解析、读不到临期就不可当选。
			if c["realId"] != "gfmodel" {
				t.Fatalf("候选丢了真实 id: %v", c["realId"])
			}
			if c["price"] != 0.6 {
				t.Fatalf("倍率没从展示名解出来: %v", c["price"])
			}
			if c["eligible"] == true {
				t.Fatalf("没有额度来源的候选被判可当选: %v/%v", c["channel"], c["realId"])
			}
		}
	}
	if !found {
		t.Fatalf("qoder 的 GLM-5.3-Flash 没归到 glm-5-3-flash（展示名通道没生效）")
	}
}

// 设置是三项独立开关，前端每次只发它改的那一项；漏发的那两项必须保持原值。
func TestAggregateSettingsPatchKeepsUnsetFields(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	aggPost(t, s, "/admin/api/aggregate/settings", `{"enabled":true}`)
	if !s.Store.Config().AggregateEnabled {
		t.Fatal("enabled 未写入")
	}
	aggPost(t, s, "/admin/api/aggregate/settings",
		`{"excluded":["workbuddy|deepseek-v4.1-flash"],"manualRank":{"deepseek-v4-1-flash":["qoder","workbuddy"]}}`)
	cfg := s.Store.Config()
	if len(cfg.AggregateExclude) != 1 || cfg.AggregateExclude[0] != "workbuddy|deepseek-v4.1-flash" {
		t.Fatalf("AggregateExclude = %v", cfg.AggregateExclude)
	}
	if got := cfg.AggregateManualRank["deepseek-v4-1-flash"]; len(got) != 2 || got[0] != "qoder" {
		t.Fatalf("AggregateManualRank = %v", cfg.AggregateManualRank)
	}
	if !cfg.AggregateEnabled {
		t.Fatal("第二次只发了 excluded/manualRank，enabled 却被清掉了")
	}
	// 全量覆盖语义：写回空数组 = 全部放回。
	aggPost(t, s, "/admin/api/aggregate/settings", `{"excluded":[],"manualRank":{}}`)
	if len(s.Store.Config().AggregateExclude) != 0 {
		t.Fatalf("清空后 AggregateExclude = %v", s.Store.Config().AggregateExclude)
	}
}

// 重算接口必须真的丢掉缓存并回一张新表：前端点「重算临期」后直接用它刷新界面。
func TestAggregateRecheckReturnsFreshState(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	out := aggPost(t, s, "/admin/api/aggregate/recheck", "{}")
	if out["ok"] != true {
		t.Fatalf("ok = %v", out["ok"])
	}
	if _, ok := out["recomputed"]; !ok {
		t.Fatal("recheck 没回 recomputed（前端要用它写 toast）")
	}
	st, _ := out["state"].(map[string]any)
	if st == nil {
		t.Fatal("recheck 没回 state")
	}
	if _, ok := st["virtual"]; !ok {
		t.Fatal("recheck 的 state 缺 virtual")
	}
}

// 手工位次严格优先于临期：拖过的渠道必须排到没拖过的之前，否则面板显示的
// 顺序与真实轮换顺序分叉，而用户以为自己已经改好了。
func TestAggregateChannelOrderPrefersManual(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateManualRank = map[string][]string{
			"some-model": {"workbuddy", "qoder"},
		}
	})
	order := s.aggChannelOrder(s.Store.Config().AggregateManualRank, s.aggExpiries(true))
	if len(order) < 2 {
		t.Fatalf("order = %v", order)
	}
	if order[0] != "workbuddy" || order[1] != "qoder" {
		t.Fatalf("order = %v, want 手工序 workbuddy,qoder 打头", order)
	}
}

// 价格写进 JSON 时不能出现 Infinity：JSON 没有那个字面量，编码会整份失败，
// 面板拿到的是一个 500 而不是「没标倍率」。
func TestAggregatePriceEncodesInfAsNull(t *testing.T) {
	payload, err := json.Marshal(map[string]any{"price": priceJSON(aggregate.Inf())})
	if err != nil {
		t.Fatalf("Inf 倍率无法编码: %v", err)
	}
	if string(payload) != `{"price":null}` {
		t.Fatalf("payload = %s", payload)
	}
	if v := priceJSON(0.75); v != 0.75 {
		t.Fatalf("priceJSON(0.75) = %v", v)
	}
}
