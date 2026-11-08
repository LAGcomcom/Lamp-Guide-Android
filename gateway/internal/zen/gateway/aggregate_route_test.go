package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

// 聚合的**执行侧**测试：链怎么排、什么进链、什么被剔除、第一站接住之后还会不会
// 继续往下试。这几条一旦错了，面板上的「备用链」就成了一张与真实路由无关的图 ——
// 而错误是静默的（请求打到了另一家，用户只看到答案不一样）。

// aggHooks 固定两个注入点并保证测试结束后归还，避免污染同包其它测试。
func aggHooks(t *testing.T, expiries func(bool) map[string]lane.QuotaExpiry, accounts func(string) int) {
	t.Helper()
	aggTestExpiries = expiries
	aggTestAccounts = accounts
	t.Cleanup(func() { aggTestExpiries = nil; aggTestAccounts = nil })
}

// 一个能接住的 qoder 目录 + 两个不能接的渠道：链上只该留下能接的那个。
func TestAggregateChainExcludesChannelsWithoutExpiry(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = append(c.Providers, store.Provider{
			ID: qoderProviderID, Name: "Qoder 账号渠道", Protocol: store.ProtocolOpenAI,
			Enabled: true, Models: []string{"gfmodel"},
			ModelMeta: map[string]store.ModelMeta{"gfmodel": {Name: "GLM-5.3-Flash · x0.6"}},
			BaseURL:   qoderGatewayURL + "/v1",
		})
	})
	aggHooks(t,
		func(bool) map[string]lane.QuotaExpiry {
			// qoder 有临期、workbuddy 读不到、qodercn 有但没账号。
			return map[string]lane.QuotaExpiry{
				"qoder":     {Known: true, ExpiryMS: time.Now().Add(2 * time.Hour).UnixMilli()},
				"qodercn":   {Known: true, ExpiryMS: time.Now().Add(time.Hour).UnixMilli()},
				"workbuddy": {},
			}
		},
		func(id string) int {
			if id == "qoder" {
				return 1
			}
			return 0
		})
	chain := s.aggregateChain(aggregatePrefix + "glm-5-3-flash")
	if len(chain) != 1 {
		t.Fatalf("chain = %d 站, want 1（只有 qoder 既有临期又有账号）", len(chain))
	}
	if chain[0].channel != "qoder" || chain[0].provider == nil || chain[0].model != "gfmodel" {
		t.Fatalf("chain[0] = %+v", chain[0])
	}
	if got := chain[0].servedID(); got != qoderProviderID+"/gfmodel" {
		t.Fatalf("servedID = %q", got)
	}
}

// 两条渠道都有临期时，先烧快到期的站：这是这一层的全部价值。
func TestAggregateChainOrdersBySoonestExpiry(t *testing.T) {
	up := fakeUpstream(t, []string{`{"choices":[{"delta":{"content":"ok"}}]}`, "[DONE]"})
	defer up.Close()
	s := newTestServer(t, up)
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = append(c.Providers, store.Provider{
			ID: qoderProviderID, Name: "Qoder 账号渠道", Protocol: store.ProtocolOpenAI,
			Enabled: true, Models: []string{"gfmodel"},
			ModelMeta: map[string]store.ModelMeta{"gfmodel": {Name: "GLM-5.3-Flash · x0.6"}},
			BaseURL:   up.URL,
		})
	})
	// qoder 与 qodercn 都在链上（共用同一个托管供应商），qoder 的临期更早 ⇒ 它先烧。
	aggHooks(t,
		func(bool) map[string]lane.QuotaExpiry {
			return map[string]lane.QuotaExpiry{
				"workbuddy": {Known: true, ExpiryMS: time.Now().Add(72 * time.Hour).UnixMilli()},
				"qoder":     {Known: true, ExpiryMS: time.Now().Add(time.Hour).UnixMilli()},
			}
		},
		func(string) int { return 1 })
	s.Store.Mutate(func(c *store.Config) {
		c.WorkBuddy.Enabled = true
		c.WorkBuddy.AccessToken = "test-token"
	})
	chain := s.aggregateChain(aggregatePrefix + "glm-5-3-flash")
	if len(chain) == 0 {
		t.Fatal("glm-5-3-flash 的链是空的")
	}
	if chain[0].channel != "qoder" {
		t.Fatalf("chain[0] = %v, want qoder（它的临期更早）", chain[0].channel)
	}
}

// 手工位次把 workbuddy 拖到前面：链的第一站必须跟着变。
// 若临期仍然优先，用户拖动会被下一轮探测**静默覆盖** —— 面板顺序与真实路由分叉。
func TestAggregateChainHonoursManualRank(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	// 用独立的 qoder 与 backup 供应商验证手工位次。
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = append(c.Providers, store.Provider{ID: "backup", Enabled: true, Protocol: store.ProtocolOpenAI, BaseURL: "http://127.0.0.1:1/v1", Models: []string{"glm-5.3-flash"}, ModelMeta: map[string]store.ModelMeta{"glm-5.3-flash": {Name: "GLM-5.3-Flash"}}})
		c.Providers = append(c.Providers, store.Provider{
			ID: qoderProviderID, Name: "Qoder 账号渠道", Protocol: store.ProtocolOpenAI,
			Enabled: true, Models: []string{"gfmodel"},
			ModelMeta: map[string]store.ModelMeta{"gfmodel": {Name: "GLM-5.3-Flash · x0.6"}},
			BaseURL:   qoderGatewayURL + "/v1",
		})
		c.AggregateManualRank = map[string][]string{"glm-5-3-flash": {"backup", "qoder"}}
	})
	aggHooks(t,
		func(bool) map[string]lane.QuotaExpiry {
			return map[string]lane.QuotaExpiry{
				"qoder":  {Known: true, ExpiryMS: time.Now().Add(time.Hour).UnixMilli()},
				"backup": {Known: true, ExpiryMS: time.Now().Add(72 * time.Hour).UnixMilli()},
			}
		},
		func(string) int { return 1 })
	chain := s.aggregateChain(aggregatePrefix + "glm-5-3-flash")
	if len(chain) < 2 {
		t.Fatalf("chain = %d 站, want 2（qoder 与 backup）", len(chain))
	}
	if chain[0].channel != "backup" {
		t.Fatalf("chain[0] = %v, want backup（手工拖动优先于临期）", chain[0].channel)
	}
	// 反过来拖一次：qoder 回到第一位，证明这是排序而不是巧合。
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateManualRank = map[string][]string{"glm-5-3-flash": {"qoder", "backup"}}
	})
	chain = s.aggregateChain(aggregatePrefix + "glm-5-3-flash")
	if chain[0].channel != "qoder" {
		t.Fatalf("重新排序后 chain[0] = %v, want qoder", chain[0].channel)
	}
}

// 第一站吐出内容之后再失败，不得继续换下一站：换家会让用户看到两份开头，
// 而半句话收尾比换一家重开更接近用户的预期。
func TestAggregateTurnStopsAfterContent(t *testing.T) {
	// 两站指向两个不同的上游，各自数自己被打了几次 —— 「换了几家」就成了可测量的
	// 事实，而不是从模型名去猜（两站可以用同一个模型名）。
	type upstream struct {
		srv  *httptest.Server
		hits int32
	}
	mk := func(broken bool) *upstream {
		u := &upstream{}
		u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&u.hits, 1)
			w.Header().Set("content-type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"半句话\"}}]}\n\n"))
			if !broken {
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			}
		}))
		return u
	}
	first := mk(false) // 吐出内容后正常收尾
	second := mk(true)
	defer first.srv.Close()
	defer second.srv.Close()

	s := newTestServer(t, first.srv)
	chain := []aggTarget{
		{channel: "qoder", realID: "m", provider: &store.Provider{
			ID: "a1", Protocol: store.ProtocolOpenAI, BaseURL: first.srv.URL}, model: "m"},
		{channel: "qodercn", realID: "m", provider: &store.Provider{
			ID: "a2", Protocol: store.ProtocolOpenAI, BaseURL: second.srv.URL}, model: "m"},
	}
	var got int
	outcome, uerr := s.aggregateTurn(context.Background(), lane.Request{
		Messages: []lane.Message{{Role: lane.RoleUser, Parts: []lane.Part{lane.TextPart{Text: "hi"}}}},
	}, chain, func(lane.Chunk) { got++ })
	if got == 0 {
		t.Fatal("没有收到任何 chunk")
	}
	if uerr != nil {
		t.Fatalf("第一站能接住却报错: %v", uerr.Message)
	}
	if outcome.ServedModel != "a1/m" {
		t.Fatalf("ServedModel = %q, want a1/m", outcome.ServedModel)
	}
	if n := atomic.LoadInt32(&first.hits); n != 1 {
		t.Fatalf("第一站被请求 %d 次, want 1", n)
	}
	if n := atomic.LoadInt32(&second.hits); n != 0 {
		t.Fatalf("第一站已经接住，第二站仍被请求 %d 次（用户会看到两份开头）", n)
	}
}

// 第一站彻底失败的形态必须换下一站：这才是「备用链」存在的理由。
func TestAggregateTurnFailsOverOnEmptyFailure(t *testing.T) {
	var hits int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"答住了\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
	}))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer first.Close()

	s := newTestServer(t, first)
	s.Store.Config() // 确保 store 已初始化
	chain := []aggTarget{
		{channel: "qoder", realID: "m", provider: &store.Provider{
			ID: "a1", Protocol: store.ProtocolOpenAI, BaseURL: first.URL}, model: "m"},
		{channel: "qodercn", realID: "m", provider: &store.Provider{
			ID: "a2", Protocol: store.ProtocolOpenAI, BaseURL: second.URL}, model: "m"},
	}
	outcome, uerr := s.aggregateTurn(context.Background(), lane.Request{
		Messages: []lane.Message{{Role: lane.RoleUser, Parts: []lane.Part{lane.TextPart{Text: "hi"}}}},
	}, chain, func(lane.Chunk) {})
	if uerr != nil {
		t.Fatalf("第二站应当接住，却报了 %v", uerr.Message)
	}
	if outcome.ServedModel != "a2/m" {
		t.Fatalf("ServedModel = %q, want a2/m", outcome.ServedModel)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("第二站被请求 %d 次, want 1", n)
	}
}

// 开关关着时，aggregate/ 名字必须立刻回错误，而不是被当成普通模型名发给 lane：
// 后者会让上游回 404，用户看到「模型不存在」，真实原因却是聚合没开。
func TestAggregateModelRequiresSwitch(t *testing.T) {
	up := fakeUpstream(t, []string{`{"choices":[{"delta":{"content":"should not run"}}]}`, "[DONE]"})
	defer up.Close()
	s := newTestServer(t, up)
	s.Store.Mutate(func(c *store.Config) { c.AggregateEnabled = false })

	body := `{"model":"aggregate/auto","messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("authorization", "Bearer "+s.Store.Config().MainKey)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code == 200 {
		t.Fatalf("关着开关还回了 200：%s", rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	errObj, _ := out["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("错误体不是 OpenAI 形状: %s", rec.Body.String())
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "聚合") {
		t.Fatalf("错误文案没有指向聚合开关: %q", msg)
	}
}

// 开关打开、但没有任何能接的候选时，错误里要带「还剩哪几家」的指路，
// 而不是一句 bare quota error。
func TestAggregateModelWithEmptyChainSaysWhy(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.Store.Mutate(func(c *store.Config) { c.AggregateEnabled = true })
	aggHooks(t, func(bool) map[string]lane.QuotaExpiry { return map[string]lane.QuotaExpiry{} },
		func(string) int { return 0 })
	out, uerr := s.runTurn(context.Background(), "aggregate/auto", lane.Request{}, func(lane.Chunk) {})
	defer func() { _ = out }()
	if uerr == nil {
		t.Fatal("没有候选却成功了")
	}
	if !strings.Contains(uerr.Message, "候选") && !strings.Contains(uerr.Message, "登录") {
		t.Fatalf("错误文案没有指路: %q", uerr.Message)
	}
}

// /v1/models 在开关打开时必须列出 aggregate/auto 与有候选的规范名；关着时不列。
func TestListModelsAdvertisesAggregateWhenOn(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = append(c.Providers, store.Provider{
			ID: qoderProviderID, Name: "Qoder 账号渠道", Protocol: store.ProtocolOpenAI,
			Enabled: true, Models: []string{"gfmodel"},
			ModelMeta: map[string]store.ModelMeta{"gfmodel": {Name: "GLM-5.3-Flash · x0.6"}},
			BaseURL:   qoderGatewayURL + "/v1",
		})
	})
	aggHooks(t,
		func(bool) map[string]lane.QuotaExpiry {
			return map[string]lane.QuotaExpiry{"qoder": {Known: true, ExpiryMS: time.Now().Add(time.Hour).UnixMilli()}}
		},
		func(id string) int {
			if id == "qoder" {
				return 1
			}
			return 0
		})
	models := listModelIDs(t, s, true)
	if !models["aggregate/auto"] {
		t.Fatalf("没有列出 aggregate/auto: %v", models)
	}
	if !models["aggregate/glm-5-3-flash"] {
		t.Fatalf("没有列出 aggregate/glm-5-3-flash: %v", models)
	}
	models = listModelIDs(t, s, false)
	for id := range models {
		if strings.HasPrefix(id, "aggregate/") {
			t.Fatalf("开关关着却列出了 %q", id)
		}
	}
}

func listModelIDs(t *testing.T, s *Server, on bool) map[string]bool {
	t.Helper()
	s.Store.Mutate(func(c *store.Config) { c.AggregateEnabled = on })
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("authorization", "Bearer "+s.Store.Config().MainKey)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("/v1/models → %d", rec.Code)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, d := range out.Data {
		ids[d.ID] = true
	}
	return ids
}
