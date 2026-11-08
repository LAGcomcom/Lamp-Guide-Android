package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"zenui/internal/zen/chprov"
	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

// 这一组测的是同一类缺陷：凭据写下了一处，而真正读它的那一处没接上，且不报错。
// 三个入口（设备授权、贴 Key、从插件导入）各写各的格子，转发层/目录各读各的格子；
// 少接一条，症状恒定是「面板显示已登录，模型却发不出去」，而用户没有任何线索。

// qoder 的贴 Key 路径必须把凭据推到内置转发层的那个文件：转发层每个请求都重读它，
// 只写 ChannelAccounts 的话，下一次模型请求仍然拿旧凭据（或干脆没有文件）。
func TestQoderTokenPasteFeedsTheSidecarCredentialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "qoder-credential.json")
	prev := chprov.QoderSidecarAuthFile
	chprov.QoderSidecarAuthFile = path
	t.Cleanup(func() { chprov.QoderSidecarAuthFile = prev })

	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.Store.Mutate(func(c *store.Config) { c.Providers = nil })

	body := `{"id":"qoder","token":"dt-token","extra":{"cosy_key":"CK",` +
		`"encrypt_user_info":"EI","machine_id":"MID","uid":"UID"}}`
	aggPost(t, s, "/admin/api/channel/token", body)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("贴了 Key 之后转发层凭据文件不存在：%v", err)
	}
	var doc struct {
		Auth map[string]string `json:"auth"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("凭据文件不是合法 JSON：%v", err)
	}
	if doc.Auth["cosy_key"] != "CK" || doc.Auth["access_token"] != "dt-token" {
		t.Fatalf("转发层拿到的凭据不对：%v", doc.Auth)
	}
}

// 整家退出必须把转发层那份文件一起收走：留着它等于「面板说退出了，而请求仍在
// 用那份凭据发出去」。
func TestQoderLogoutClearsTheSidecarCredentialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "qoder-credential.json")
	prev := chprov.QoderSidecarAuthFile
	chprov.QoderSidecarAuthFile = path
	t.Cleanup(func() { chprov.QoderSidecarAuthFile = prev })

	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	if err := chprov.WriteQoderSidecarCredential(chprov.Account{
		AccessToken: "dt-token", Source: "paste",
		Extra: map[string]string{"cosy_key": "CK", "encrypt_user_info": "EI", "machine_id": "MID", "uid": "UID"},
	}); err != nil {
		t.Fatalf("前置条件不成立：%v", err)
	}
	s.Store.Mutate(func(c *store.Config) {
		c.ChannelAccounts = map[string]chprov.Accounts{"qoder": {{
			ID: "UID", AccessToken: "dt-token",
			Extra: map[string]string{"cosy_key": "CK", "encrypt_user_info": "EI", "machine_id": "MID", "uid": "UID"},
		}}}
	})
	aggPost(t, s, "/admin/api/channel/logout", `{"id":"qoder"}`)

	if _, err := os.Stat(path); err == nil {
		t.Fatal("退出整家后转发层凭据文件仍在：请求会继续用那份凭据")
	}
}

// 设置页那个开关关掉再打开，不能把渠道面登录留下的凭据清掉。
//
// 根因：开关推给 lane 的是 cfg.WorkBuddy，而渠道面登录写的是
// channelAccounts.workbuddy —— 两份都合法，但只有一份被读过。
func TestWorkBuddyToggleKeepsTheChannelCredential(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	token := wbIntlJWT(t, "uid-wb", 1900000000)
	s.saveChannelAccount("workbuddy", chprov.Account{
		AccessToken: token, RefreshTok: "rt-wb", ID: "uid-wb", Source: "poll",
	}, true)

	aggPost(t, s, "/admin/api/settings", `{"workBuddyEnabled":false}`)
	if ids := wbRoster(t, s.Lane); len(ids) != 0 {
		t.Fatalf("开关关掉后目录里仍有 WorkBuddy 模型：%v", ids)
	}
	aggPost(t, s, "/admin/api/settings", `{"workBuddyEnabled":true}`)
	if ids := wbRoster(t, s.Lane); len(ids) != 3 {
		t.Fatalf("开关重新打开后目录里应有 3 条模型，实际 %d 条：%v", len(ids), ids)
	}
	if tok := s.Lane.WorkBuddySnapshot().AccessToken; tok != token {
		t.Fatal("开关走了一圈，lane 手里的凭据被换掉了")
	}
}

// 设置页的开关必须落盘：只改内存快照的话，重启就回到旧值，而界面上当时已经生效。
func TestWorkBuddyToggleIsPersisted(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	aggPost(t, s, "/admin/api/settings", `{"workBuddyEnabled":false}`)

	reopened, err := store.Open()
	if err != nil {
		t.Fatalf("重开配置失败：%v", err)
	}
	if reopened.Config().WorkBuddy.Enabled {
		t.Fatal("workBuddyEnabled=false 没有落盘：重启后会自己回来")
	}
}

// 设置页退出必须把两张表都清掉：凭据可能只落在 channelAccounts.workbuddy
// （渠道面那条登录路径就是写它），重启时 core 会把它折回账号。
func TestWorkBuddyLogoutClearsBothCredentialTables(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	token := wbIntlJWT(t, "uid-wb", 1900000000)
	s.saveChannelAccount("workbuddy", chprov.Account{
		AccessToken: token, RefreshTok: "rt-wb", ID: "uid-wb", Source: "poll",
	}, true)
	if ids := wbRoster(t, s.Lane); len(ids) == 0 {
		t.Fatal("前置条件不成立：登录后应当有模型")
	}

	aggPost(t, s, "/admin/api/workbuddy/logout", `{}`)

	cfg := s.Store.Config()
	if len(cfg.ChannelAccounts["workbuddy"].Usable()) != 0 || cfg.WorkBuddy.AccessToken != "" {
		t.Fatalf("退出后仍留着凭据：channelAccounts=%d 条, workBuddy=%q",
			len(cfg.ChannelAccounts["workbuddy"]), cfg.WorkBuddy.AccessToken)
	}
	wbGone(t, s.Lane)
}

// wbGone 等目录里的 WorkBuddy 模型消失。退出之后的重建是异步的（与登录同一个
// 理由），所以这里等的是「退出这个动作最终会收回模型」，而不是调用返回时它们
// 已经不在了 —— 后者会把一次正常的异步当成缺陷。
func wbGone(t *testing.T, l *lane.Lane) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var ids []string
		for _, m := range l.ServableModels() {
			if m.Channel == lane.ChannelWorkBuddy {
				ids = append(ids, m.ID)
			}
		}
		if len(ids) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("退出后目录里仍有 WorkBuddy 模型：%v", ids)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// 状态卡读的必须是折合后的账号，否则「渠道面登录成功、模型也在选择器里」的机器
// 会在设置卡上显示成「未登录」。
func TestWorkBuddyStatusSeesTheChannelCredential(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.saveChannelAccount("workbuddy", chprov.Account{
		AccessToken: wbIntlJWT(t, "uid-wb", 1900000000), RefreshTok: "rt-wb",
		ID: "uid-wb", Nickname: "wb-user", Source: "poll",
	}, true)

	out := aggGet(t, s, "/admin/api/workbuddy/status")
	if out["loggedIn"] != true {
		t.Fatalf("渠道面已登录，状态卡却报未登录：%v", out)
	}
	if out["nickname"] != "wb-user" {
		t.Fatalf("nickname = %v", out["nickname"])
	}
}

func TestWorkBuddyDisabledStatusMatchesAggregateAndKeepsLogin(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.WorkBuddy.Enabled = false
		c.ChannelAccounts = map[string]chprov.Accounts{"workbuddy": {{
			ID: "review-wb", AccessToken: "review-dummy", Source: "poll",
		}}}
	})
	out := aggGet(t, s, "/admin/api/workbuddy/status")
	if out["loggedIn"] != true || out["enabled"] != false {
		t.Fatalf("disabled model access must preserve login but report disabled: logged=%v enabled=%v", out["loggedIn"], out["enabled"])
	}
	if n := s.aggAccounts("workbuddy"); n != 0 {
		t.Fatalf("disabled WorkBuddy has %d routable accounts", n)
	}
	s.Store.Mutate(func(c *store.Config) { c.WorkBuddy.Enabled = true })
	if n := s.aggAccounts("workbuddy"); n != 1 {
		t.Fatalf("enabling model access must recover stored account, got %d", n)
	}
}

// 面板上的「可当选」与执行侧进链的判据必须同源：托管供应商被停用时，候选不能
// 还亮着 eligible —— 那是「面板有、候选表永远轮不到」的静默分叉。
func TestAggregateCandidateNotEligibleWhenProviderDisabled(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.AggregateEnabled = true
		c.Providers = append(c.Providers, store.Provider{
			ID: qoderProviderID, Name: "Qoder 账号渠道", Protocol: store.ProtocolOpenAI,
			Enabled: false, Models: []string{"gfmodel"},
			ModelMeta: map[string]store.ModelMeta{"gfmodel": {Name: "GLM-5.3-Flash"}},
			BaseURL:   qoderGatewayURL + "/v1",
		})
	})
	aggHooks(t,
		func(bool) map[string]lane.QuotaExpiry {
			return map[string]lane.QuotaExpiry{"qoder": {Known: true, ExpiryMS: 1}}
		},
		func(string) int { return 1 })

	for _, vm := range s.buildAggregateState(true)["virtual"].([]map[string]any) {
		for _, c := range vm["candidates"].([]map[string]any) {
			if c["channel"] == "qoder" && c["eligible"] == true {
				t.Fatal("供应商已停用，候选却标着可当选")
			}
		}
	}
}

// 导出时凭据被抹成占位符，导入时必须同样清掉。
//
// 漏了这一段，症状是「导入别人的配置之后，面板显示 7 家已登录、还剩 N 天」，
// 而每一次请求都 401 —— 占位符被当成真凭据存进了凭据表，且零报错。
func TestImportDropsMaskedChannelCredentials(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.ChannelAccounts = map[string]chprov.Accounts{
			"workbuddy": {{ID: "u1", AccessToken: "real-token", RefreshTok: "real-rt",
				Extra: map[string]string{"uid": "u1"}}},
			"zcode": {{ID: "u2", AccessToken: "real-z", Extra: map[string]string{"device_mid": "m"}}},
		}
		c.WorkBuddy = lane.WBAccount{AccessToken: "real-wb", RefreshToken: "real-wb-rt", Enabled: true}
	})
	masked := aggGetRaw(t, s, "/admin/api/export")
	aggPost(t, s, "/admin/api/import", string(masked))

	cfg := s.Store.Config()
	for id, list := range cfg.ChannelAccounts {
		for _, a := range list {
			if a.AccessToken == store.MaskedValue || a.RefreshTok == store.MaskedValue {
				t.Fatalf("%s 的凭据把占位符当成真令牌存下来了", id)
			}
			if a.Extra["uid"] == store.MaskedValue || a.Extra["device_mid"] == store.MaskedValue {
				t.Fatalf("%s 的附加字段把占位符存下来了", id)
			}
		}
	}
	if cfg.WorkBuddy.AccessToken == store.MaskedValue {
		t.Fatal("WorkBuddy 的令牌把占位符存下来了")
	}
	// 占位符清完之后没有任何可用材料的条目要整条丢掉，否则面板会显示「1 个账号」。
	if len(cfg.ChannelAccounts["workbuddy"]) != 0 {
		t.Fatalf("空凭据没有被丢掉：%+v", cfg.ChannelAccounts["workbuddy"])
	}
}

// aggGetRaw 取原始响应体（导出面回的不是 JSON 对象）。
func aggGetRaw(t *testing.T, s *Server, path string) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1"
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET %s → %d", path, rec.Code)
	}
	return rec.Body.Bytes()
}

func TestWorkBuddyLegacyRowCanLogoutIndividually(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{"[DONE]"}))
	a := lane.WBAccount{UID: "legacy-u", AccessToken: "legacy-token", Enabled: true}
	s.Store.Mutate(func(c *store.Config) { c.WorkBuddy = a })
	s.Lane.SetWorkBuddyAccount(a)
	if len(s.chanAccounts("workbuddy")) != 1 {
		t.Fatal("legacy account missing from UI")
	}
	out := chanPost(t, s, "/admin/api/channel/logout", `{"id":"workbuddy","key":"id:legacy-u"}`)
	if out["remaining"] != float64(0) || s.Store.Config().WorkBuddy.AccessToken != "" || s.Lane.WorkBuddySnapshot().AccessToken != "" {
		t.Fatal("legacy account still active after row logout")
	}
}

func TestQoderAliasLogoutDisablesLastProviderImmediately(t *testing.T) {
	old := chprov.QoderSidecarAuthFile
	chprov.QoderSidecarAuthFile = filepath.Join(t.TempDir(), "auth.json")
	t.Cleanup(func() { chprov.QoderSidecarAuthFile = old })
	s := newTestServer(t, fakeUpstream(t, []string{"[DONE]"}))
	s.Store.Mutate(func(c *store.Config) {
		c.ChannelAccounts = map[string]chprov.Accounts{"qodercn": {{ID: "legacy-q", AccessToken: "token"}}}
		c.Providers = []store.Provider{{ID: qoderProviderID, Enabled: true, Models: []string{"m"}}}
	})
	chanPost(t, s, "/admin/api/channel/logout", `{"id":"qoder","key":"id:legacy-q"}`)
	if s.providerByID(qoderProviderID).Enabled {
		t.Fatal("provider remains enabled after last alias logout")
	}
	s.ensureQoderProvider(qoderStatus{LoggedIn: true, ModelIDs: []string{"m"}})
	if s.providerByID(qoderProviderID).Enabled {
		t.Fatal("stale status resurrected logged-out provider")
	}
	if len(s.chanQuotas("qoder")) != 0 {
		t.Fatal("explicit logout fell back to sidecar quota")
	}
}
