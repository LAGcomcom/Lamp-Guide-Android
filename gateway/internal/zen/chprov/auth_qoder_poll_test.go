package chprov

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Qoder 的设备授权有三段（授权页 → poll 换 token → userinfo → 铸 cosy）。
// 这条测试把**前两段**用假上游钉住，重点是一处曾经真实报错的契约：
//
// 上游的成功响应里字段叫 token（客户端读 u.token + u.refresh_token 判成功），
// 而我们只认 device_token，于是浏览器那边确认完、授权也已生效，这里却报
// 「登录失败：换 token 响应里没有 device_token」。
//
// § 另两处也一并钉住：
//   404 是「用户还没点确认」，必须继续等，而不是失败；
//   401/403 才是真拒绝（流程作废）。
func TestPollQoderAcceptsUpstreamTokenField(t *testing.T) {
	var sawVerifier string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/deviceToken/poll":
			sawVerifier = r.URL.Query().Get("verifier")
			if r.URL.Query().Get("challenge_method") != "S256" {
				w.WriteHeader(400)
				return
			}
			// 上游真实形状：token + refresh_token（不是 device_token）。
			_, _ = w.Write([]byte(`{"token":"dt-abc","refresh_token":"rt-abc","expires_in":3600}`))
		case "/api/v1/userinfo":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"id":"uid-1","name":"tester","email":"t@e.c"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	prevOpen, prevAuth := qoderOpenAPIBase, qoderAuthBase
	qoderOpenAPIBase = srv.URL
	qoderAuthBase = srv.URL
	defer func() { qoderOpenAPIBase, qoderAuthBase = prevOpen, prevAuth }()

	prevFile := QoderSidecarAuthFile
	QoderSidecarAuthFile = t.TempDir() + "/cred.json"
	defer func() { QoderSidecarAuthFile = prevFile }()

	spec, _ := SpecOf("qoder")
	sess := LoginSession{
		Channel: "qoder", Kind: LoginQoder,
		State: "nonce-1", Verifier: "verifier-1", DeviceID: "machine-1",
	}
	// 这条走完整条链路：换 token → userinfo → 铸 cosy → 写 sidecar 凭据文件。
	// 「换到 token 被当成失败」会在第一步就报出来，而后面几步各自还有别的测试。
	state, acct, err := pollQoder(context.Background(), spec, sess)
	if err != nil {
		t.Fatalf("字段名 token 的成功响应被判成失败：%v", err)
	}
	if state != "ok" {
		t.Fatalf("状态 %q，期望 ok", state)
	}
	if acct.AccessToken != "dt-abc" {
		t.Fatalf("令牌没搬进凭据: %q", acct.AccessToken)
	}
	if acct.ID != "uid-1" {
		t.Fatalf("uid 没从 userinfo 读回来（cosy 凭据要靠它铸）: %q", acct.ID)
	}
	// cosy 那份是模型请求真正签的那一份：缺一格都会让请求 401。
	if acct.Field("cosy_key") == "" || acct.Field("encrypt_user_info") == "" {
		t.Fatalf("cosy 凭据没铸出来: %v", acct.Extra)
	}
	if sawVerifier != "verifier-1" {
		t.Fatalf("轮询没带 verifier（PKCE 校验会失败）：%q", sawVerifier)
	}
}

// 404 是正常态（用户还没点确认），必须回 pending 而不是报错。
func TestPollQoderTreatsNotFoundAsPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	prevOpen := qoderOpenAPIBase
	qoderOpenAPIBase = srv.URL
	defer func() { qoderOpenAPIBase = prevOpen }()

	spec, _ := SpecOf("qoder")
	state, _, err := pollQoder(context.Background(), spec, LoginSession{State: "n", Verifier: "v"})
	if err != nil {
		t.Fatalf("404 应当回 pending，实际报错：%v", err)
	}
	if state != "pending" {
		t.Fatalf("404 的状态应为 pending，实际 %q", state)
	}
}

// 401/403 是真拒绝：继续轮询只会让用户等一个永远不会来的结果。
func TestPollQoderTreatsUnauthorizedAsRejection(t *testing.T) {
	for _, code := range []int{401, 403} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		prevOpen := qoderOpenAPIBase
		qoderOpenAPIBase = srv.URL

		spec, _ := SpecOf("qoder")
		_, _, err := pollQoder(context.Background(), spec, LoginSession{State: "n", Verifier: "v"})
		srv.Close()
		qoderOpenAPIBase = prevOpen

		if err == nil {
			t.Fatalf("HTTP %d 必须报错（流程已作废）", code)
		}
		if !strings.Contains(err.Error(), "作废") && !strings.Contains(err.Error(), "拒绝") {
			t.Fatalf("HTTP %d 的错误没有说明「需要重新登录」: %v", code, err)
		}
	}
}

// 字段名对不上时，报错必须把「上游到底给了什么形状」说出来 —— 否则一次字段
// 改名会被读成一次登录失败，而下一个排查的人只能抓包。
func TestPollQoderFieldMismatchNamesTheShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"accessToken":"x","uid":"u1"}`))
	}))
	defer srv.Close()

	prevOpen := qoderOpenAPIBase
	qoderOpenAPIBase = srv.URL
	defer func() { qoderOpenAPIBase = prevOpen }()

	spec, _ := SpecOf("qoder")
	_, _, err := pollQoder(context.Background(), spec, LoginSession{State: "n", Verifier: "v"})
	if err == nil {
		t.Fatal("没有可用 token 时必须报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, "accessToken") {
		t.Fatalf("报错没有列出响应的真实字段名，排查只能靠抓包: %v", err)
	}
}
