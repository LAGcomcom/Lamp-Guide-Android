package lane

import (
	"testing"
	"time"

	"zenui/internal/zen/chprov"
)

// WorkBuddy 的登录有两条入口，各写各的格子。这条测试钉住「渠道表的凭据能被 lane
// 认出来」这一环 —— 少了它，用户在渠道面登录成功、面板显示已登录，而模型选择器
// 里一条 WorkBuddy 模型都没有（workBuddyReady 看的是那份空的 config.workBuddy）。
func TestWBAccountFromChannelFeedsReadyLane(t *testing.T) {
	token := wbJWT("uid-42", "https://www.workbuddy.ai", 1900000000)
	accts := chprov.Accounts{{
		AccessToken: token, RefreshTok: "rt-42", ID: "uid-42",
		Nickname: "小号", ExpiresAtMS: 1700000000000,
		Source: "poll",
	}}

	acct := WBAccountFromChannel(accts, WBAccount{Enabled: false})
	if !acct.Enabled {
		t.Fatal("渠道表里有凭据时账号必须是启用态：否则目录不会出现这几条模型")
	}
	if acct.AccessToken != token || acct.RefreshToken != "rt-42" {
		t.Fatalf("令牌对没搬过来: %+v", acct)
	}
	// 到期时刻以 JWT 自己那一格为准（field 可能是秒、也可能缺）。
	if got, want := acct.ExpiresAtMS, int64(1900000000)*1000; got != want {
		t.Fatalf("到期时刻 = %d, want %d（JWT 的 exp 优先）", got, want)
	}

	// 折出来之后 lane 必须真的「就绪」：这是整条链路唯一的判定点。
	l := NewLane()
	l.SetWorkBuddyAccount(acct)
	if _, ok := l.workBuddyReady(); !ok {
		t.Fatalf("装进 lane 之后仍判未登录: %+v", l.WorkBuddySnapshot())
	}
}

// 空表不能把账号「折」成启用态：那会让没登录的用户看到三条用不了的模型。
func TestWBAccountFromChannelKeepsFallbackWhenEmpty(t *testing.T) {
	fallback := WBAccount{AccessToken: "old-token", UID: "u1", Enabled: true, LastActivityDate: "2026-11-04"}
	if got := WBAccountFromChannel(nil, fallback); got.AccessToken != "old-token" || !got.Enabled {
		t.Fatalf("空表时应当原样返回 fallback，实际 %+v", got)
	}
	// 只有空壳（token 为空）也算「没有」。
	if got := WBAccountFromChannel(chprov.Accounts{{ID: "u2"}}, fallback); got.AccessToken != "old-token" {
		t.Fatalf("无 token 的条目不该覆盖 fallback，实际 %+v", got)
	}
}

// 重登同一账号要保住当天的签到记录；换账号则不能继承（那是另一个号的活跃日）。
func TestWBAccountFromChannelCarriesActivityOnlyForSameUID(t *testing.T) {
	token := wbJWT("uid-42", "https://www.workbuddy.ai", 1900000000)
	same := WBAccountFromChannel(
		chprov.Accounts{{AccessToken: token, ID: "uid-42"}},
		WBAccount{UID: "uid-42", LastActivityDate: "2026-11-04"})
	if same.LastActivityDate != "2026-11-04" {
		t.Fatalf("同一账号重登丢了活跃日: %+v", same)
	}
	other := WBAccountFromChannel(
		chprov.Accounts{{AccessToken: token, ID: "uid-42"}},
		WBAccount{UID: "someone-else", LastActivityDate: "2026-11-04"})
	if other.LastActivityDate != "" {
		t.Fatalf("换了账号却继承了上一个号的活跃日: %+v", other)
	}
}

// 缺 uid 时不能留空：指纹种子从 uid 派生，空值会让每次请求换一副面孔（WAF 直接拦）。
func TestWBAccountFromChannelNeverLeavesUIDEmpty(t *testing.T) {
	token := wbJWT("", "https://www.workbuddy.ai", 1900000000)
	acct := WBAccountFromChannel(chprov.Accounts{{AccessToken: token}}, WBAccount{})
	if acct.UID == "" {
		t.Fatal("uid 为空：机器指纹会每次漂移")
	}
	_ = time.Now
}
