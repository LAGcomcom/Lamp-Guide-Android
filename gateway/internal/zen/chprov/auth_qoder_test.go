package chprov

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// Qoder 的内置登录有三处「看起来成功、其实发不出请求」的陷阱，全部在这条
// 测试里钉住：登录形态登记、授权页形状、以及凭据落盘时写的 schema。
//
// ⚠️ 第三处最隐蔽：sidecar 读的是它自己的 schema（source / auth{cosy_key,…}），
// 若我们直接把 Account 的结构写下去，它会判成「缺 cosy_key」并**静默**退回 IDE
// 缓存 —— 用户看到「登录成功」，而模型请求用的仍是旧那份。

// 两站都必须走内置设备授权，而不是「贴 Key 观察 IDE」。
func TestQoderChannelsUseBuiltInDeviceLogin(t *testing.T) {
	for _, id := range []string{"qoder", "qodercn"} {
		spec, ok := SpecOf(id)
		if !ok {
			t.Fatalf("渠道 %s 没有登记", id)
		}
		if spec.Login.Kind != LoginQoder {
			t.Fatalf("%s 的登录形态是 %q，应为内置设备授权 %q", id, spec.Login.Kind, LoginQoder)
		}
		if spec.Login.AuthURL == "" {
			t.Fatalf("%s 缺授权页地址（面板上会只剩一个点了没反应的「登录」）", id)
		}
		// 这四格是登录成功后由网关现场铸/读回来的，必须登记在 ExtraKeys 里，
		// 否则面板会把它们当成「用户该填却没填」的字段，挂一条消不掉的警告。
		want := map[string]bool{"cosy_key": false, "encrypt_user_info": false, "machine_id": false, "uid": false}
		for _, k := range spec.ExtraKeys {
			if _, ok := want[k]; ok {
				want[k] = true
			}
		}
		for k, seen := range want {
			if !seen {
				t.Fatalf("%s 的 ExtraKeys 缺 %s", id, k)
			}
		}
	}
}

// 授权页必须是「登录页 + oauth_callback 包住 device/selectAccounts」这一形状。
//
// ⚠️ 直接给 selectAccounts 时，未登录用户看到的是空白而不是登录框；而把回调用
// 别的路径包，用户登录完了也回不到设备授权这一步。两者都表现为「点了登录，
// 页面打开了，什么也没发生」。
func TestStartQoderBuildsWrappedAuthorizeURL(t *testing.T) {
	spec, _ := SpecOf("qoder")
	sess, err := startQoder(context.Background(), spec)
	if err != nil {
		t.Fatalf("发起登录失败：%v", err)
	}
	defer Forget("qoder")

	if sess.Kind != LoginQoder {
		t.Fatalf("会话形态 %q", sess.Kind)
	}
	u, err := url.Parse(sess.OpenURL)
	if err != nil {
		t.Fatalf("授权页不是合法 URL：%v", err)
	}
	if u.Host != "qoder.cn" || u.Path != "/users/sign-in" {
		t.Fatalf("授权页应落在 qoder.cn/users/sign-in，实际 %s%s", u.Host, u.Path)
	}
	back := u.Query().Get("oauth_callback")
	if back == "" {
		t.Fatal("登录页缺 oauth_callback：用户登录完成后没有回跳目标")
	}
	inner, err := url.Parse(back)
	if err != nil {
		t.Fatalf("oauth_callback 不是合法 URL：%v", err)
	}
	if inner.Path != "/device/selectAccounts" {
		t.Fatalf("回跳应指向 /device/selectAccounts，实际 %s", inner.Path)
	}
	for _, k := range []string{"challenge", "challenge_method", "nonce", "machine_id", "client_id"} {
		if inner.Query().Get(k) == "" {
			t.Fatalf("设备授权缺参数 %s", k)
		}
	}
	if inner.Query().Get("challenge_method") != "S256" {
		t.Fatalf("challenge_method 必须是 S256，实际 %q", inner.Query().Get("challenge_method"))
	}
	// verifier 只在进程内，不能出现在任何给用户看的 URL 里。
	if sess.Verifier == "" {
		t.Fatal("会话里没有 verifier：轮询时换不出 token")
	}
	if msg := sess.OpenURL; len(msg) > 0 && contains(msg, sess.Verifier) {
		t.Fatal("授权页 URL 里泄露了 code_verifier")
	}
}

// 凭据落盘写的是 **sidecar 的 schema**，不是 Account 的形状。
func TestQoderCredentialFileUsesSidecarSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cred.json")
	prev := QoderSidecarAuthFile
	QoderSidecarAuthFile = path
	defer func() { QoderSidecarAuthFile = prev }()

	acct := Account{
		AccessToken: "dt-token", Source: LoginQoder, ExpiresAtMS: 1793807318000,
		Extra: map[string]string{
			"cosy_key": "CK", "encrypt_user_info": "EI", "machine_id": "MID", "uid": "UID",
		},
	}
	if err := writeQoderSidecarCredential(acct); err != nil {
		t.Fatalf("写凭据失败：%v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回凭据失败：%v", err)
	}
	var doc struct {
		Source          string            `json:"source"`
		TokenExpireTime string            `json:"token_expire_time"`
		Auth            map[string]string `json:"auth"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("凭据不是合法 JSON：%v", err)
	}
	if doc.Source != LoginQoder {
		t.Fatalf("source 应是 %q，实际 %q", LoginQoder, doc.Source)
	}
	for k, want := range map[string]string{
		"cosy_key": "CK", "encrypt_user_info": "EI", "user_id": "UID",
		"machine_id": "MID", "access_token": "dt-token",
	} {
		if doc.Auth[k] != want {
			t.Fatalf("auth.%s = %q，应为 %q", k, doc.Auth[k], want)
		}
	}
	// 秒与毫秒混用会让 sidecar 算出「1970 年就过期」，登录当场被判失效。
	if doc.TokenExpireTime != "1793807318000" {
		t.Fatalf("token_expire_time 应按毫秒写，实际 %q", doc.TokenExpireTime)
	}
}

// 没配置落盘路径时**报错**而不是静默成功：静默成功会让登录看起来完成了，
// 而中继仍在用旧凭据读。
func TestWriteQoderCredentialFailsWithoutPath(t *testing.T) {
	prev := QoderSidecarAuthFile
	QoderSidecarAuthFile = ""
	defer func() { QoderSidecarAuthFile = prev }()
	if err := writeQoderSidecarCredential(Account{AccessToken: "t"}); err == nil {
		t.Fatal("没有落盘路径时必须报错")
	}
}

func contains(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
