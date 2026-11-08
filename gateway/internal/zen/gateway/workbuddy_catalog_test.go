package gateway

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"zenui/internal/zen/chprov"
	"zenui/internal/zen/lane"
	"zenui/internal/zen/store"
)

// wbIntlJWT 造一个国际版 WorkBuddy 令牌（iss 必须是 workbuddy.ai，uid 在 sub 里）。
func wbIntlJWT(t *testing.T, uid string, expSec int64) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"sub": uid, "iss": "https://www.workbuddy.ai", "exp": expSec,
	})
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	return head + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

// wbRoster 等目录里出现 WorkBuddy 那三条模型。
//
// 重建目录是异步的（它要打上游、写盘，不该拖住登录响应），所以这里等的是
// 「登录这个动作最终会让模型出现」，而不是「调用返回时它已经在了」——后者
// 会把一次正常的异步当成缺陷。
//
// ⚠️ 超时必须明显长于 lane 的 coldBootRetryDelay（3 秒）：冷启动时那两条
// listing 会各自失败一次再等 3 秒重试，一趟重建就要 3 秒出头。卡在 3 秒会
// 让这条测试在机器忙时随机变红，而它要证明的那件事（登录最终通向目录）是
// 稳的 —— 一条随机变红的守卫测试会被下一个人直接删掉。
func wbRoster(t *testing.T, l *lane.Lane) []string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var ids []string
		for _, m := range l.ServableModels() {
			if m.Channel == lane.ChannelWorkBuddy {
				ids = append(ids, m.ID)
			}
		}
		if len(ids) > 0 || time.Now().After(deadline) {
			return ids
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// 用户报告的真实症状：在渠道面登录成功、面板显示「已登录」，可模型选择器里
// 一条 WorkBuddy 模型都没有。
//
// 根因是凭据写进了两张表：渠道面写 channelAccounts.workbuddy，而模型目录由
// lane 的账号快照（config.workBuddy）决定。少了桥接这一步，登录与目录之间没有
// 任何连线，症状与「没登录」在界面上完全一样，且**不报任何错**。
func TestWorkBuddyLoginReachesTheCatalog(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	token := wbIntlJWT(t, "uid-wb", 1900000000)

	// 走真实路径：渠道面的登录结果落到凭据表，由 saveChannelAccount 触发同步。
	s.saveChannelAccount("workbuddy", chprov.Account{
		AccessToken: token, RefreshTok: "rt-wb", ID: "uid-wb", Source: "poll",
	}, true)

	found := wbRoster(t, s.Lane)
	if len(found) != 3 {
		t.Fatalf("WorkBuddy 登录后目录里应有 3 条模型，实际 %d 条：%v", len(found), found)
	}
}

// 聚合面必须跟着出现 WorkBuddy 的候选：否则「登录了但没有模型」在聚合里照样成立。
func TestWorkBuddyLoginReachesAggregateCandidates(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	token := wbIntlJWT(t, "uid-wb", 1900000000)
	s.saveChannelAccount("workbuddy", chprov.Account{
		AccessToken: token, RefreshTok: "rt-wb", ID: "uid-wb", Source: "poll",
	}, true)

	if ids := wbRoster(t, s.Lane); len(ids) == 0 {
		t.Fatal("目录里没有 WorkBuddy 模型，聚合面必然也没有")
	}
	var hit bool
	for _, vm := range s.buildAggregateState(true)["virtual"].([]map[string]any) {
		for _, c := range vm["candidates"].([]map[string]any) {
			if c["channel"] == "workbuddy" {
				hit = true
			}
		}
	}
	if !hit {
		t.Fatal("聚合候选里没有 workbuddy：账号在、模型在，候选表却说没有")
	}
}

// 退出必须把模型一起收回：留着三条用不了的模型会让用户以为号还在。
func TestWorkBuddyLogoutRetractsTheCatalog(t *testing.T) {
	s := newTestServer(t, fakeUpstream(t, []string{`{"choices":[]}`, "[DONE]"}))
	token := wbIntlJWT(t, "uid-wb", 1900000000)
	s.saveChannelAccount("workbuddy", chprov.Account{
		AccessToken: token, RefreshTok: "rt-wb", ID: "uid-wb", Source: "poll",
	}, true)
	if ids := wbRoster(t, s.Lane); len(ids) == 0 {
		t.Fatal("前置条件不成立：登录后应当有模型")
	}

	s.Store.Mutate(func(c *store.Config) {
		delete(c.ChannelAccounts, "workbuddy")
		c.WorkBuddy = lane.WBAccount{}
	})
	s.Lane.WorkBuddyLogout()
	s.Lane.RefreshCatalog(t.Context())

	if ids := wbRoster(t, s.Lane); len(ids) != 0 {
		t.Fatalf("退出后目录里仍有 WorkBuddy 模型：%v", ids)
	}
}
