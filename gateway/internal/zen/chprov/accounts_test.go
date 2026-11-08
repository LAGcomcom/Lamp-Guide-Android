package chprov

import (
	"encoding/json"
	"strings"
	"testing"
)

// 多账号是这一层最容易被写错的地方：一个渠道只留一个槽位，症状是
// 「导入读到 22 条、面板上只有 7 家」——那 15 条被静默覆盖，零报错。
// 另一个更隐蔽：Key 只用 AccessToken 时，换过一次令牌的同一个账号会变成
// 两行，用户的退出动作只清掉其中一行。

// Upsert 同 Key 覆盖、不同 Key 追加：这是「加一个账号」的全部语义。
func TestAccountsUpsertKeepsOneRowPerAccount(t *testing.T) {
	var as Accounts
	first := Account{ID: "u1", AccessToken: "tok-a", Nickname: "主号"}
	if added := as.Upsert(first); !added {
		t.Fatal("第一条应当算新增")
	}
	// 同一个账号换了令牌：不该多出一行。
	rotated := Account{ID: "u1", AccessToken: "tok-a2", Nickname: "主号"}
	if added := as.Upsert(rotated); added {
		t.Fatal("同一账号换令牌被当成新账号：面板上会出现两行同一个号")
	}
	if as[0].AccessToken != "tok-a2" {
		t.Fatalf("令牌没被更新: %q", as[0].AccessToken)
	}
	// 第二个账号：必须真的多一行。
	second := Account{ID: "u2", AccessToken: "tok-b", Nickname: "小号"}
	if added := as.Upsert(second); !added {
		t.Fatal("第二个账号没被加进去")
	}
	if len(as) != 2 {
		t.Fatalf("accounts = %d 行, want 2", len(as))
	}
}

// Key 必须优先用上游给的账号 id：只按令牌算时，刷新一次令牌就让同一个号
// 变成两个「账号」，而用户的退出动作只清掉其中一个。
func TestAccountKeyPrefersUpstreamIdentity(t *testing.T) {
	a := Account{ID: "u1", AccessToken: "tok-a"}
	b := Account{ID: "u1", AccessToken: "tok-a-rotated"}
	if a.Key() != b.Key() {
		t.Fatalf("换令牌后 Key 变了: %q → %q", a.Key(), b.Key())
	}
	// 没有 ID 时退到 uid（腾讯 buddy 系只给 uid）。
	c := Account{Extra: map[string]string{"uid": "ub"}, AccessToken: "x"}
	d := Account{Extra: map[string]string{"uid": "ub"}, AccessToken: "y"}
	if c.Key() != d.Key() {
		t.Fatalf("按 uid 的 Key 不稳定: %q vs %q", c.Key(), d.Key())
	}
	// 完全没有身份字段时按令牌摘要区分两台机器，且不同令牌必须不同 Key。
	e := Account{AccessToken: "tok-1"}
	f := Account{AccessToken: "tok-2"}
	if e.Key() == f.Key() {
		t.Fatal("两台不同设备的凭据被合并成同一个 Key")
	}
}

// 老配置里 channelAccounts 的每个值是**一条对象**。按数组解会让整份 config.json
// 被判成损坏（loadJSON 会把它改名成 .corrupt-*），用户看到全部凭据一夜清空。
func TestAccountsUnmarshalAcceptsLegacyObject(t *testing.T) {
	// 这是 channelAccounts 里**某一个渠道**的值（外层那个 map 由 store 解）。
	legacy := []byte(`{"accessToken":"jwt-a","nickname":"Auto","extra":{"device_mid":"mid"}}`)
	var as Accounts
	if err := json.Unmarshal(legacy, &as); err != nil {
		t.Fatalf("老格式（单条对象）解析失败: %v", err)
	}
	if len(as) != 1 || as[0].AccessToken != "jwt-a" || as[0].Extra["device_mid"] != "mid" {
		t.Fatalf("解析结果不对: %+v", as)
	}
	// 新格式（数组）照常解析。
	current := []byte(`[{"accessToken":"a"},{"accessToken":"b"}]`)
	var as2 Accounts
	if err := json.Unmarshal(current, &as2); err != nil {
		t.Fatalf("新格式解析失败: %v", err)
	}
	if len(as2) != 2 {
		t.Fatalf("新格式解析出 %d 条, want 2", len(as2))
	}
	// null 与空串不该炸：旧配置里这个字段可能压根没有。
	var as3 Accounts
	if err := json.Unmarshal([]byte("null"), &as3); err != nil {
		t.Fatalf("null 解析失败: %v", err)
	}
	if len(as3) != 0 {
		t.Fatalf("null 应当是空集，得到 %d 条", len(as3))
	}
}

// 退出要能只退一条：整家清掉会把用户还要留着的另一个号一起删掉。
func TestAccountsRemoveOneKeepsTheRest(t *testing.T) {
	as := Accounts{
		{ID: "u1", AccessToken: "a"},
		{ID: "u2", AccessToken: "b"},
	}
	next, hit := as.Remove("id:u1")
	if !hit {
		t.Fatal("没删掉 id:u1")
	}
	if len(next) != 1 || next[0].ID != "u2" {
		t.Fatalf("剩下的不是 u2: %+v", next)
	}
}

// 无昵称时标签要能区分两台设备，而不是「账号 1 / 账号 2」那种两台机器上都一样的
// 匿名编号 —— 用户分不出该退哪一个。
func TestAccountLabelFallsBackToIdentityTail(t *testing.T) {
	a := Account{AccessToken: "tok-abcdefghijklmnop"}
	if got := a.Label(); got == "" || strings.Contains(got, tokensPlaceholderForTest()) {
		t.Fatalf("标签不可用: %q", got)
	}
}

// tokensPlaceholderForTest 让上面那条断言读起来是「标签里不能出现令牌本体」。
func tokensPlaceholderForTest() string { return "tok-abcdefghijklmnop" }
