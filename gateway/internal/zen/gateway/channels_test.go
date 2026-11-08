package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"zenui/internal/zen/chprov"
	"zenui/internal/zen/store"
)

// channels_test 守的是渠道面的三条事实：
//
//	1. 一家渠道的**多个账号**要逐条出现在 accountList 里（额度按账号算，
//	   只报一条会让面板说不出「哪个号先烧完」）；
//	2. 退出要能**只退一个号**（整家退出会把用户还要留着的那个一起清掉）；
//	3. 写操作之后额度读数必须作废（否则贴错 Key 之后面板还显示「还剩 20 天」）。
//
// ⚠️ 这里选 qiniu / groq 这类 TierNone 的渠道做夹具：它们的额度端点根本不存在，
// ReadQuota 不打网络，于是断言在任何机器上都能跑，而不是「本机登录过才跑」。

func chanGet(t *testing.T, s *Server, path string) map[string]any {
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

func chanPost(t *testing.T, s *Server, path, body string) map[string]any {
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

// chanRow 取 channel/state 里某一家那一条。
func chanRow(t *testing.T, s *Server, id string) map[string]any {
	t.Helper()
	st := chanGet(t, s, "/admin/api/channel/state")
	rows, _ := st["channels"].([]any)
	for _, r := range rows {
		row, _ := r.(map[string]any)
		if row["id"] == id {
			return row
		}
	}
	t.Fatalf("channel/state 里没有 %s", id)
	return nil
}

// TestChannelStateListsEveryAccount 是同一条渠道两个号时面板必须给出的东西：
// 两条 accountList、accountCount=2、每条各自带一份额度读数，且**不回显令牌**。
func TestChannelStateListsEveryAccount(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{"[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.ChannelAccounts = map[string]chprov.Accounts{
			"qiniu": {
				{AccessToken: "key-a", ID: "uid-a", Nickname: "主号", Source: "paste"},
				{AccessToken: "key-b", ID: "uid-b", Nickname: "小号", Source: "import"},
			},
		}
	})
	if err := s.Store.Save(); err != nil {
		t.Fatal(err)
	}

	row := chanRow(t, s, "qiniu")
	list, _ := row["accountList"].([]any)
	if len(list) != 2 {
		t.Fatalf("accountList = %d 条，期望 2 条（这一家有两个号）", len(list))
	}
	if got := row["accountCount"]; got != float64(2) {
		t.Fatalf("accountCount = %v, want 2", got)
	}
	// 逐条的标签要能分清两个号：两行都写「账号 1」时用户不知道该退哪一个。
	labels := map[string]bool{}
	for _, it := range list {
		a, _ := it.(map[string]any)
		labels[a["label"].(string)] = true
		if a["source"] == "" {
			t.Fatalf("账号 %v 没有来源（排查「为什么不会自动续期」第一眼看它）", a["label"])
		}
		// 每条都要有自己的额度读数（TierNone 是 Never，不是「没读」）。
		q, _ := a["quota"].(map[string]any)
		if q == nil || q["never"] != true {
			t.Fatalf("账号 %v 的额度读数 = %v，期望 never", a["label"], a["quota"])
		}
	}
	if len(labels) != 2 {
		t.Fatalf("两个号的标签重复了: %v", labels)
	}
	// 面板不回显令牌：一次截图就会把全部凭据带走。
	raw := chanGet(t, s, "/admin/api/channel/state")
	b, _ := json.Marshal(raw)
	if strings.Contains(string(b), "key-a") || strings.Contains(string(b), "accessToken") {
		t.Fatal("channel/state 回显了令牌本体")
	}
}

// 退出一个账号不能把同家的另一个号一起清掉 —— 那个号是用户还要留着的，
// 而面板上他点的只是清单里的某一行。
func TestChannelLogoutOneAccountKeepsTheOther(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{"[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.ChannelAccounts = map[string]chprov.Accounts{
			"groq": {
				{AccessToken: "k1", ID: "u1", Nickname: "主号"},
				{AccessToken: "k2", ID: "u2", Nickname: "小号"},
			},
		}
	})
	if err := s.Store.Save(); err != nil {
		t.Fatal(err)
	}
	out := chanPost(t, s, "/admin/api/channel/logout", `{"id":"groq","key":"id:u1"}`)
	if got := out["remaining"]; got != float64(1) {
		t.Fatalf("remaining = %v, want 1（另一个号必须还在）", got)
	}
	if got := out["removed"]; got != float64(1) {
		t.Fatalf("removed = %v, want 1", got)
	}
	row := chanRow(t, s, "groq")
	if got := row["accountCount"]; got != float64(1) {
		t.Fatalf("退出一个号后 accountCount = %v, want 1", got)
	}
	// 不带 key 才是整家退出：那是面板上「整家」那个入口，后果差好几个号。
	out = chanPost(t, s, "/admin/api/channel/logout", `{"id":"groq"}`)
	if got := out["remaining"]; got != float64(0) {
		t.Fatalf("整家退出后 remaining = %v, want 0", got)
	}
}

// 贴 Key 之后上一分钟的读数必须作废：否则贴错的 Key 会顶着旧账号的
// 「还剩 20 天」显示，用户会以为已经接入成功。
func TestChannelTokenInvalidatesQuotaCache(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{"[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.ChannelAccounts = map[string]chprov.Accounts{
			"qiniu": {{AccessToken: "old", ID: "uid-old"}},
		}
	})
	if err := s.Store.Save(); err != nil {
		t.Fatal(err)
	}
	// 先读一次，把读数写进缓存。
	if got := len(s.chanQuotas("qiniu")); got != 1 {
		t.Fatalf("首次读额度得到 %d 条", got)
	}
	out := chanPost(t, s, "/admin/api/channel/token", `{"id":"qiniu","token":"new","nickname":"新号"}`)
	if out["ok"] != true {
		t.Fatalf("贴 Key 失败: %v", out["error"])
	}
	if got := out["added"]; got != true {
		t.Fatalf("第二个号应该算「新增」，added = %v", got)
	}
	if got := out["accountCount"]; got != float64(2) {
		t.Fatalf("贴第二个 Key 之后 accountCount = %v, want 2", got)
	}
	// 缓存里不能留下旧账号的那条读数：它已经不在这一家里了。
	qs := s.chanQuotas("qiniu")
	if _, ok := qs["id:uid-old"]; !ok {
		t.Fatalf("旧账号被贴 Key 顶掉了，缓存里却还在: %v", qs)
	}
	if len(qs) != 2 {
		t.Fatalf("两个号都有凭据，读到的却只有 %d 条", len(qs))
	}
}

// 「我接了 4 家供应商，渠道页为什么没有 4 家」——这一面必须自己回答。
//
// 两份数据是分开的：供应商是用户填的端点（relay 直连，只要 Key），渠道是编译期
// 那 30 家的清单（要登录、有额度端点）。只有 id 同名时才重叠。第 4 家（intern）
// 不在渠道表里，症状是"面板上少了一家"，而原因不是漏显示。
func TestChannelStateCrosswalksProviders(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{"[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.Providers = []store.Provider{
			{ID: "nvidia-nim", Name: "NVIDIA NIM", Enabled: true, Models: []string{"m1"}},
			{ID: "qoder", Name: "Qoder 账号渠道", Enabled: true, Models: []string{"m2"}},
			{ID: "intern", Name: "书生·端砚 Intern", Enabled: true, Models: []string{"m3", "m4"}},
		}
	})
	if err := s.Store.Save(); err != nil {
		t.Fatal(err)
	}
	st := chanGet(t, s, "/admin/api/channel/state")
	rows, _ := st["providers"].([]any)
	if len(rows) != 3 {
		t.Fatalf("providers = %d 条，期望 3 条（供应商逐家都要交代）", len(rows))
	}
	// 同名的那两家要标出来，且带上渠道名；intern 必须标成「没有同名渠道」。
	seen := map[string]bool{}
	for _, it := range rows {
		row, _ := it.(map[string]any)
		id, _ := row["id"].(string)
		linked, _ := row["channel"].(bool)
		seen[id] = linked
		if linked && row["channelName"] == "" {
			t.Fatalf("%s 标了同名渠道却没有渠道名", id)
		}
	}
	if !seen["nvidia-nim"] || !seen["qoder"] {
		t.Fatalf("这两家与渠道表同名，应标 channel=true: %v", seen)
	}
	if seen["intern"] {
		t.Fatal("intern 不在渠道表里，不该标成有同名渠道")
	}
	totals, _ := st["totals"].(map[string]any)
	if got := totals["providers"]; got != float64(3) {
		t.Fatalf("totals.providers = %v, want 3", got)
	}
	if got := totals["shared"]; got != float64(2) {
		t.Fatalf("totals.shared = %v, want 2（只有两家同名）", got)
	}
}
