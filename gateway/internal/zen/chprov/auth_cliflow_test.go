package chprov

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// zcode 的登录口此前是「点了没反应」：描述表把它登记成 LoginPoll，而 LoginPoll
// 的请求体是 {}。上游那条端点要的是 {"provider":"zai"} 加一个本机铸的 64 位
// pollToken，回 400 code 3001。这份测试把两段式合同逐条钉住 —— 三条约束各对应
// 一个真实错误码，少一条就退回「发起登录失败：HTTP 400」。
func TestCliFlowSendsProviderAndLongPollToken(t *testing.T) {
	type seen struct {
		body   string
		bearer string
	}
	var initSeen, pollSeen seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		raw, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/init"):
			initSeen = seen{body: string(raw), bearer: bearer}
			// 上游的硬约束：Bearer 必须是 64 字符（32 字节 hex）。
			if len(bearer) != 64 {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"code":3004,"msg":"invalid_flow"}`))
				return
			}
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if body["provider"] != "zai" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"code":3001,"msg":"parameter error"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"flow_id":"f1","poll_token":"` + bearer +
				`","authorize_url":"https://chat.z.ai/api/oauth/authorize?state=s1",` +
				`"expires_at":` + "1234567890" + `,"poll_interval_sec":2}}`))
		case strings.Contains(r.URL.Path, "/poll/"):
			pollSeen = seen{bearer: bearer}
			// 换 token 时 Bearer 必须还是同一个 pollToken，否则上游认不出这条流程。
			if bearer != initSeen.bearer {
				_, _ = w.Write([]byte(`{"code":3004,"msg":"invalid_flow"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"status":"ready","token":"jwt.token.value",` +
				`"user":{"user_id":"u1","name":"主号"},"zai":{"access_token":"zai-token"}}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	spec := Spec{
		ID: "zcode",
		Login: LoginSpec{
			Kind: LoginCliFlow, Provider: "zai",
			StartURL: srv.URL + "/oauth/cli/init",
			PollURL:  srv.URL + "/oauth/cli/poll",
		},
	}
	sess, err := startCliFlow(context.Background(), spec)
	if err != nil {
		t.Fatalf("发起失败: %v", err)
	}
	if sess.State != "f1" || sess.OpenURL == "" {
		t.Fatalf("会话缺 flow_id 或授权页: %+v", sess)
	}
	if sess.IntervalMS != 2000 {
		t.Fatalf("轮询间隔应取上游的 poll_interval_sec=2，实际 %d", sess.IntervalMS)
	}
	if sess.ExpiresAtMS <= 0 {
		t.Fatalf("截止时刻没从 expires_at 折算: %d", sess.ExpiresAtMS)
	}
	// pollToken 不能回显给前端：它换 token 的唯一凭据。
	if strings.Contains(mustJSON(t, sess), sess.PollToken) {
		t.Fatalf("pollToken 出现在会话 JSON 里: %s", mustJSON(t, sess))
	}

	state, acct, err := pollCliFlow(context.Background(), spec, sess)
	if err != nil {
		t.Fatalf("轮询失败: %v", err)
	}
	if state != "ok" {
		t.Fatalf("状态 %q，期望 ok", state)
	}
	if acct.AccessToken != "jwt.token.value" {
		t.Fatalf("AccessToken 应是 zcode 的 jwt，实际 %q", acct.AccessToken)
	}
	// 额度端点认 jwt，不认 zai access token（实测 401）；两份都要落盘，
	// 但落盘时各自待在各自的位置上。
	if acct.Extra["zai_access_token"] != "zai-token" {
		t.Fatalf("zai access token 没进 Extra: %+v", acct.Extra)
	}
	// device_mid 必须随登录一起落盘：额度端点缺它得 400 code 3001，缺着就等于
	// 「登录成功了但读不到额度」，而那两句在面板上分不开。
	if acct.Extra["device_mid"] == "" {
		t.Fatalf("device_mid 没随登录铸出来: %+v", acct.Extra)
	}

	if acct.ID != "u1" && acct.ID != "" {
		t.Fatalf("账号 id 应是 user_id 或留空，实际 %q", acct.ID)
	}
	if acct.Nickname != "主号" {
		t.Fatalf("昵称没读出来: %q", acct.Nickname)
	}
	if acct.Source != LoginCliFlow {
		t.Fatalf("来源应记为 %q，实际 %q", LoginCliFlow, acct.Source)
	}
	// 上游对 pending 的回答必须继续等，不能当失败。
	if initSeen.body == "" || pollSeen.bearer == "" {
		t.Fatalf("两段都该被真的调到: init=%q poll=%q", initSeen.body, pollSeen.bearer)
	}
}

// 上游说 failed 时必须报错并丢掉会话：挂着一个已经作废的流程再轮一万次，
// 用户看到的是「一直转圈」。
func TestCliFlowFailedStatusClearsSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"status":"failed"}}`))
	}))
	defer srv.Close()
	spec := Spec{ID: "zcode", Login: LoginSpec{Kind: LoginCliFlow, Provider: "zai",
		StartURL: srv.URL + "/init", PollURL: srv.URL + "/poll"}}
	sess := LoginSession{Channel: "zcode", Kind: LoginCliFlow, State: "f1",
		PollToken: "deadbeef", StartedAtMS: 1}
	remember(sess)
	if _, _, err := pollCliFlow(context.Background(), spec, sess); err == nil {
		t.Fatal("failed 状态应当返回错误")
	}
	if _, ok := Session("zcode"); ok {
		t.Fatal("failed 之后会话应当被丢掉（否则面板会一直轮询一条死流程）")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	return string(b)
}
