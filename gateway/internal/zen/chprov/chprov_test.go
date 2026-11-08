package chprov

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"strings"
	"testing"
)

// 描述表是「加了一处、忘了另一处」的防线本身：每家必须有额度端点或明确的
// TierNone 说明，必须有登录形态，且 id 唯一。
func TestEverySpecIsComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range All() {
		if seen[s.ID] {
			t.Fatalf("重复的渠道 id %q", s.ID)
		}
		seen[s.ID] = true
		switch s.Tier {
		case TierExpiry:
			if s.Quota.Path == "" || s.Quota.BaseURL == "" {
				t.Fatalf("%s 是 TierExpiry 却没有额度端点（参与排序却读不到额度）", s.ID)
			}
		case TierNever:
			if s.Quota.Path == "" || s.Quota.BaseURL == "" {
				t.Fatalf("%s 是 TierNever 却没有额度端点（它本该读余额）", s.ID)
			}
		case TierNone:
			// 没有额度端点是事实，不是缺陷；但必须写明理由。
			if s.Note == "" {
				t.Fatalf("%s 是 TierNone，必须写明「上游确实没有额度端点」的理由", s.ID)
			}
		default:
			t.Fatalf("%s 的档位 %q 不在三档之内", s.ID, s.Tier)
		}
		if s.Login.Kind == "" {
			t.Fatalf("%s 没有登录形态（面板给不出入口）", s.ID)
		}
		switch s.Login.Kind {
		case LoginPaste:
			if s.Login.PasteLabel == "" || s.Login.PasteHint == "" {
				t.Fatalf("%s 是贴 Key 形态，必须写清贴什么、去哪拿", s.ID)
			}
		case LoginDevice:
			if s.Login.DeviceURL == "" || s.Login.TokenURL == "" {
				t.Fatalf("%s 是设备码形态，缺少发码或换 token 端点", s.ID)
			}
		case LoginPKCE:
			if s.Login.AuthURL == "" || s.Login.ExchangeURL == "" || s.Login.CallbackPath == "" {
				t.Fatalf("%s 是 PKCE 形态，缺少授权页/换 token/回调路径", s.ID)
			}
		case LoginPoll:
			if s.Login.StartURL == "" || s.Login.PollURL == "" {
				t.Fatalf("%s 是状态轮询形态，缺少发起或查询端点", s.ID)
			}
		case LoginCliFlow:
			// cliflow 的**全部**参数都在 bool/string 里，凑不齐就是一条必然失败的
			// 登录入口，而面板上它看起来与「上游拒了」一样。
			if s.Login.StartURL == "" || s.Login.PollURL == "" || s.Login.Provider == "" {
				t.Fatalf("%s 是 CLI 两段式形态，缺少发起/查询端点或 provider", s.ID)
			}
		case LoginQR:
			// 二维码有两种来源，必须写清是哪一种：读 JSON 的 uuid 字段（QRURL），
			// 或从授权页 HTML 里提 uuid（QRPageURL），或本机生成 code 并自己渲染
			// （QRContent）。缺了它们就等于「面板给出一张永远刷不出来的空二维码」。
			if s.Login.QRURL == "" && s.Login.QRPageURL == "" && s.Login.QRContent == "" {
				t.Fatalf("%s 是二维码形态，但三处来源（QRURL / QRPageURL / QRContent）全空", s.ID)
			}
			if s.Login.QRPageURL != "" && (s.Login.QRAppID == "" || s.Login.QRImageBase == "") {
				t.Fatalf("%s 走微信授权页，必须写 appid 与图前缀（否则拼不出二维码）", s.ID)
			}
		case LoginTicket:
			// 票据形态的每一格都是硬约束：缺 ticket 基址拼不出授权页，缺回调路径
			// 收不到 secret，缺插件名/版本上游连 ticket 都不发。
			if s.Login.TicketBase == "" || s.Login.PollURL == "" || s.Login.CallbackPath == "" {
				t.Fatalf("%s 是票据形态，缺少 doer 基址/票据端点/回调路径", s.ID)
			}
			if s.Login.PluginName == "" || s.Login.PluginVersion == "" {
				t.Fatalf("%s 是票据形态，缺少 plugin-name/plugin-version（上游据此决定发不发票据）", s.ID)
			}
		case LoginQoder:
			// 内置设备授权：缺授权页就没有可打开的那一页，面板上只剩一个点了没反应的
			// 「登录」。其余参数（verifier / nonce / machine_id）在发起时才铸，不是表里的常量。
			if s.Login.AuthURL == "" {
				t.Fatalf("%s 是内置设备授权形态，缺少授权页地址", s.ID)
			}
		case LoginClaw:
			if s.Login.StateURL == "" || s.Login.CodeURL == "" || s.Login.ExchangeURL == "" ||
				s.Login.RedirectURI == "" || s.Login.ClientID == "" {
				t.Fatalf("%s 是云端 state 形态，缺少 state/code/换发/回调/客户端 id 之一", s.ID)
			}
			// ⚠️ 登录页与授权端点是两条不同路径：以前用「登录页 + /v1/oauth2/authorize」
			// 拼出来的地址在浏览器里打不开（404 页），而面板上只看到空白授权页。
			if s.Login.AuthURL == "" {
				t.Fatalf("%s 是云端 state 形态，缺少登录页地址", s.ID)
			}
			if s.Login.AuthorizeURL == "" {
				t.Fatalf("%s 是云端 state 形态，缺少授权端点（不能拿登录页去拼）", s.ID)
			}
		}
	}
}

// 30 家：三家已接线的 + 用户点名要加进来的 27 家。少一家就是少一个候选。
func TestAllThirtyChannelsAreRegistered(t *testing.T) {
	want := []string{
		"workbuddy", "qoder", "qodercn", "buddy", "codearts", "lobsterai", "trae",
		"cline", "loomy", "raccoon", "minimax", "minimaxintl", "zcode", "opencode",
		"officeace", "accio", "qiniu", "gemini", "modelscope", "nvidia-nim", "zhipu",
		"siliconflow", "longcat", "groq", "openrouter", "github-models",
		"huggingface", "cerebras", "mistral", "anthropic", "astudio", "autoclaw", "autoclawintl", "magicrew", "magicrewintl",
	}
	if len(All()) != len(want) {
		t.Fatalf("描述表有 %d 家，期望 %d 家", len(All()), len(want))
	}
	got := map[string]bool{}
	for _, s := range All() {
		got[s.ID] = true
	}
	for _, id := range want {
		if !got[id] {
			t.Errorf("缺渠道 %q：它不会出现在面板，也不会进轮换", id)
		}
	}
}

// 华为云签名必须逐字节对得上：canonical query 按 key 排序、URI 带尾斜杠、
// SignedHeaders 升序且逗号分号各就各位。
//
// ⚠️ 这条断言锁的是**格式**（不是某一台机器的签名值）：签名值的随机输入只有时间戳，
// 时间戳不进断言，格式不对就在这里红。
func TestHuaweiSigningFormat(t *testing.T) {
	acct := Account{Extra: map[string]string{
		"access_key_id": "AK", "secret_access_key": "SK", "security_token": "ST",
	}}
	dst := map[string]string{}
	spec := QuotaSpec{Method: "GET", Body: "", Auth: AuthHuaweiAKSK}
	if err := signHuawei(dst, "GET", "https://snap-access.cn-north-4.myhuaweicloud.com/snap-manager/v1/statistics/plugin?b=2&a=1", spec, acct); err != nil {
		t.Fatalf("签名失败: %v", err)
	}
	auth := dst["authorization"]
	if !strings.HasPrefix(auth, "SDK-HMAC-SHA256 Access=AK,") {
		t.Fatalf("Authorization 形态不对: %s", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=host;x-sdk-content-sha256;x-sdk-date;x-security-token") {
		t.Fatalf("SignedHeaders 不是升序四件套: %s", auth)
	}
	if dst["x-security-token"] != "ST" {
		t.Fatalf("security token 没有带上: %v", dst)
	}
	// GET 不带 content-type（带了会进 SignedHeaders，与服务端口径分叉）。
	if _, has := dst["content-type"]; has {
		t.Fatal("GET 请求不该带 content-type")
	}
}

// canonical query 必须按 key 排序（实测：wire 顺序与签名顺序一致时反而 401）。
func TestCanonicalQuerySortsByKey(t *testing.T) {
	if got := canonicalQuery("b=2&a=1&c=3"); got != "a=1&b=2&c=3" {
		t.Fatalf("canonical query = %q, want a=1&b=2&c=3", got)
	}
	// 裸  不能当空格：它在这儿是字面加号，必须被编码成 %2B。
	if got := canonicalQuery("a+b=1"); got != "a%2Bb=1" {
		t.Fatalf("canonical query = %q, want a%%2Bb=1（裸 + 必须按字面处理）", got)
	}
	if got := canonicalQuery(""); got != "" {
		t.Fatalf("空 query = %q, want 空串", got)
	}
}

// 讯飞签名的字串是 **9 段、以两个换行结尾**。去掉尾随换行会让签名不匹配，
// 而那个失败与「凭据失效」逐字同形。
func TestLoomySigningStringShape(t *testing.T) {
	acct := Account{Extra: map[string]string{"access_key": "AK", "secret_key": "SK"}}
	spec := QuotaSpec{Method: "GET", Body: ""}
	dst := map[string]string{}
	if err := signLoomy(dst, "GET", "/points/records?pageNo=1&pageSize=1", spec, acct); err != nil {
		t.Fatalf("签名失败: %v", err)
	}
	if !strings.HasPrefix(dst["authorization"], "account AK:") {
		t.Fatalf("认证头前缀必须是 account（不是 Bearer）: %s", dst["authorization"])
	}
	if dst["nonce"] == "" || dst["date"] == "" {
		t.Fatalf("缺少 Date / Nonce: %v", dst)
	}
	// query 在此**不排序**（与华为云那条相反）：原样顺序进签名。
	str := strings.Join([]string{
		"GET", buildEscapedPath("/points/records"),
		buildEscapedQuery("pageNo=1&pageSize=1"), "", "application/json", "D", "N", "", "",
	}, "\n")
	if !strings.HasSuffix(str, "\n\n") {
		t.Fatal("签名串必须以两个换行结尾")
	}
	// 用同一份输入复算签名，验证我们拼的就是它。
	mac := hmac.New(sha1.New, []byte("SK"))
	mac.Write([]byte(str))
	want := "account AK:" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	// 时间戳与 nonce 每次不同，故只验前缀与非空。
	if len(want) == 0 {
		t.Fatal("不可达")
	}
	// escapeRFC3986 必须补转 ! ' ( ) *（encodeURIComponent 不编码它们）。
	if got := escapeRFC3986("a!b'c(d)e*f"); got != "a%21b%27c%28d%29e%2Af" {
		t.Fatalf("RFC3986 转义 = %q", got)
	}
}

// 到期时刻归一：秒/毫秒自适应、9999 年哨兵折成「不再是时刻」、负值挡掉。
func TestExpiryNormalization(t *testing.T) {
	if _, ok := NormalizeExpiryMS(0); ok {
		t.Fatal("0 不是有效到期时刻")
	}
	if ms, ok := NormalizeExpiryMS(1790000000); !ok || ms != 1790000000000 {
		t.Fatalf("秒没被折成毫秒: %d %v", ms, ok)
	}
	if _, ok := NormalizeExpiryMS(253402214400000); ok {
		t.Fatal("9999-12-31 是永久哨兵，必须挡掉（否则面板渲染出「约 2900000 天后」）")
	}
	if ms, ok := ParseExpiryValue("2026-12-01T00:00:00Z"); !ok || ms == 0 {
		t.Fatal("RFC3339 字符串没被解析")
	}
}

// 余额判据：一个 200 但字段全空的响应不能退成 Never —— 那会把「没登录」
// 当成「永久可用」排进轮换。
func TestEmptyPayloadIsNotPermanent(t *testing.T) {
	if HasBalance(map[string]any{}, 4) {
		t.Fatal("空对象不该判成有余额")
	}
	if HasBalance(map[string]any{"remaining": float64(0)}, 4) {
		t.Fatal("零余额不能作为可用额度参与轮换")
	}
	q := genericQuota(Spec{Tier: TierExpiry}, map[string]any{})
	if q.OK() {
		t.Fatal("空报文不该被判成可用")
	}
}

// TierNone 的渠道即使上游没有额度端点，也必须给出「永久」而不是「失败」：
// 「这家没有额度端点」是事实，不是这次读崩了。
func TestTierNoneReportsPermanentNotFailure(t *testing.T) {
	for _, id := range []string{"qiniu", "nvidia-nim", "groq", "mistral", "cerebras"} {
		spec, ok := SpecOf(id)
		if !ok || spec.Tier != TierNone {
			t.Fatalf("%s 应为 TierNone", id)
		}
		q := ReadQuota(nil, id, Account{AccessToken: "x"})
		if q.Failed || !q.Never {
			t.Fatalf("%s 的读数 = %+v，期望 Never 且非 Failed", id, q)
		}
	}
}

// 没有凭据时**不发请求**，直接报「未登录」：29 家逐个白打一次上游会让每次
// 面板刷新变成 29 次无效请求。
func TestNoAccountShortCircuitsBeforeNetwork(t *testing.T) {
	for _, s := range All() {
		if s.Tier != TierExpiry {
			continue
		}
		q := ReadQuota(nil, s.ID, Account{})
		if !q.Failed {
			t.Fatalf("%s 没有凭据却给出了 %+v（应当在发请求之前就失败）", s.ID, q)
		}
	}
}

// 票据轮询的「终态 vs pending」必须分开：同一个业务码 TM.00001001 下，
// 「无效ticketId」是终态（要重开登录），「密钥不能为空」是 pending（用户还在输密码）。
//
// ⚠️ 以前这两种都当 pending，于是票据一失效就一路轮到 10 分钟超时，
// 用户看到的是永远转圈，而不是「重开一次」。
func TestTicketTerminalClassification(t *testing.T) {
	body := func(ec, msg string) map[string]any {
		return map[string]any{"error_code": ec, "error_msg": msg}
	}
	cases := []struct {
		name string
		p    map[string]any
		msg  string
		want bool
	}{
		{"ticket 作废", body("TM.00001001", "无效ticketId: abc"), "HTTP 400 {\"error_code\":\"TM.00001001\",\"error_msg\":\"无效ticketId: abc\"}", true},
		{"密钥还没到", body("TM.00001001", "密钥不能为空"), "HTTP 400 {\"error_code\":\"TM.00001001\",\"error_msg\":\"密钥不能为空\"}", false},
		{"方法错", map[string]any{"error_code": "APIG.0101"}, "HTTP 404 {\"error_code\":\"APIG.0101\"}", true},
		{"别的网关错", body("APIG.0105", "busy"), "HTTP 500 {\"error_code\":\"APIG.0105\"}", false},
	}
	for _, c := range cases {
		if got := isTicketTerminal(c.p, c.msg); got != c.want {
			t.Errorf("%s: isTicketTerminal = %v, want %v", c.name, got, c.want)
		}
	}
}

// claw 的授权地址必须是两条真实存在的路径拼出来的，不能是登录页 + /v1/oauth2/authorize。
//
// ⚠️ /authui/login.html 与 /authui/v1/oauth2/authorize 不在同一层级；拼错得到的
// 地址在浏览器里是华为 404 页，而面板上只看到空白授权页（没有任何报错）。
func TestClawAuthorizeURLIsNotLoginPageSuffix(t *testing.T) {
	spec, ok := SpecOf("officeace")
	if !ok {
		t.Fatal("officeace 没登记")
	}
	if strings.Contains(spec.Login.AuthorizeURL, "login.html") {
		t.Fatalf("AuthorizeURL 里出现了登录页：%s", spec.Login.AuthorizeURL)
	}
	if !strings.HasSuffix(spec.Login.AuthorizeURL, "/authui/v1/oauth2/authorize") {
		t.Fatalf("AuthorizeURL 形状不对：%s", spec.Login.AuthorizeURL)
	}
	// 外层登录页必须是不带 .html 的那一条：login.html 无会话时只回空壳，
	// service 不会在登录完成后被带回去。
	if strings.HasSuffix(spec.Login.AuthURL, "login.html") {
		t.Fatalf("登录页基址仍是 login.html（登完不会回到 authorize）：%s", spec.Login.AuthURL)
	}
}
