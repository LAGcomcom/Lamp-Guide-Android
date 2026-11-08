package chprov

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"zenui/internal/zen/checkin"
)

// CheckinCapability describes implemented behavior, not advertised marketing.
// Keep manual/captcha and unsupported channels out of automatic network calls.
func CheckinCapability(id string) (mode, reason string) {
	switch id {
	case "buddy", "codearts", "lobsterai", "trae", "loomy", "minimax", "minimaxintl", "astudio", "autoclaw", "autoclawintl", "magicrew", "magicrewintl":
		return "automatic", "支持每日领取；仅以上游确认的业务结果记账"
	case "zcode":
		return "manual", "领取需要验证码，请在官方客户端手动处理"
	case "qoder", "qodercn":
		return "manual", "本项目使用 Qoder 中国版；尚未核实该登录身份的每日活动协议，请在官方客户端领取"
	case "workbuddy":
		return "unsupported", "WorkBuddy 国际版没有每日签到接口；活跃请求不代表积分到账"
	default:
		return "unsupported", "当前未接入可核实的每日领取接口；登录奖励和自动赠送不算每日签到"
	}
}

func checkinFailure(message string) checkin.Outcome {
	return checkin.Outcome{Status: "failed", Message: message}
}
func checkinLogin() checkin.Outcome {
	return checkin.Outcome{Status: "login_required", Message: "登录凭据已失效，请重新登录后点击补签"}
}
func checkinDone(status, day string) checkin.Outcome {
	message := "上游已确认领取"
	if status == "already" {
		message = "上游已确认今日已领取"
	}
	return checkin.Outcome{Status: status, Message: message, CoveredDay: day}
}

type checkinClientKey struct{}

// WithCheckinClient lets the host use its current proxy transport without a
// package-global client swap that could race quota and login requests.
func WithCheckinClient(ctx context.Context, client *http.Client) context.Context {
	return context.WithValue(ctx, checkinClientKey{}, client)
}

// checkinRequest never follows redirects with credentials and never surfaces raw
// responses/URLs in the dashboard or persisted records (they can contain tokens).
func checkinRequest(ctx context.Context, method, endpoint string, headers map[string]string, body string) (map[string]any, int, *checkin.Outcome) {
	return checkinJSONRequest(ctx, method, endpoint, headers, body, false)
}

// Only native model catalogs may use a bare list; claim responses must remain
// objects with their explicit business-result fields.
func checkinJSONRequest(ctx context.Context, method, endpoint string, headers map[string]string, body string, allowList bool) (map[string]any, int, *checkin.Outcome) {
	var reader io.Reader
	if method != "GET" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		out := checkinFailure("领取请求构造失败")
		return nil, 0, &out
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	if method != "GET" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *Client
	if configured, ok := ctx.Value(checkinClientKey{}).(*http.Client); ok && configured != nil {
		client = *configured
	}
	client.Timeout = HTTPTimeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		out := checkinFailure("领取网络请求失败或超时，请检查渠道代理后补签")
		return nil, 0, &out
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		out := checkinLogin()
		return nil, resp.StatusCode, &out
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		out := checkinFailure("上游要求跳转，已停止转发凭据；请检查国际站出口与渠道地址")
		return nil, resp.StatusCode, &out
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	var payload map[string]any
	if err == nil && len(raw) <= maxBody && allowList && strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		var rows []any
		if json.Unmarshal(raw, &rows) == nil && rows != nil {
			return map[string]any{"models": rows}, resp.StatusCode, nil
		}
	}
	if err != nil || len(raw) > maxBody || json.Unmarshal(raw, &payload) != nil || payload == nil {
		out := checkinFailure(fmt.Sprintf("上游响应无法解析（HTTP %d）", resp.StatusCode))
		return nil, resp.StatusCode, &out
	}
	return payload, resp.StatusCode, nil
}

func checkinCode(payload map[string]any, key string) string {
	v := payload[key]
	switch n := v.(type) {
	case string:
		return n
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return ""
}

func claimRequest(ctx context.Context, s QuotaSpec, a Account) (map[string]any, int, *checkin.Outcome) {
	headers, err := authHeaders(s, a)
	if err != nil {
		out := checkinLogin()
		return nil, 0, &out
	}
	return checkinRequest(ctx, s.Method, s.URL(a), headers, s.Body)
}

// CheckinAccount refreshes only a verified OAuth refresh flow. Other expired
// credentials require login instead of inventing a refresh URL.
func CheckinAccount(ctx context.Context, id string, a Account) (Account, *checkin.Outcome) {
	if a.Empty() {
		out := checkinLogin()
		return a, &out
	}
	if a.ExpiresAtMS == 0 || a.ExpiresAtMS > time.Now().Add(time.Minute).UnixMilli() {
		return a, nil
	}
	if id == "autoclaw" || id == "autoclawintl" {
		fresh, err := refreshAutoclaw(ctx, id, a)
		if err == nil {
			return fresh, nil
		}
		out := checkinLogin()
		return a, &out
	}
	if (id != "minimax" && id != "minimaxintl") || a.RefreshTok == "" {
		out := checkinLogin()
		return a, &out
	}
	s, _ := SpecOf(id)
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {a.RefreshTok}, "client_id": {s.Login.ClientID}, "scope": {s.Login.Scope}, "audience": {"agent-backend"}}
	p, status, failure := checkinRequest(ctx, "POST", s.Login.TokenURL, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, form.Encode())
	if failure != nil {
		return a, failure
	}
	if status < 200 || status >= 300 || field(p, "access_token") == "" {
		out := checkinLogin()
		return a, &out
	}
	a.AccessToken = field(p, "access_token")
	if refresh := field(p, "refresh_token"); refresh != "" {
		a.RefreshTok = refresh
	}
	a.ExpiresAtMS = 0
	if seconds, ok := numeric(p["expires_in"]); ok && seconds > 0 {
		a.ExpiresAtMS = time.Now().Add(time.Duration(seconds) * time.Second).UnixMilli()
	}
	return a, nil
}

func ClaimDaily(ctx context.Context, id string, a Account, day string) checkin.Outcome {
	mode, reason := CheckinCapability(id)
	if mode != "automatic" {
		return checkin.Outcome{Status: mode, Message: reason}
	}
	if a.Empty() || (a.ExpiresAtMS > 0 && a.ExpiresAtMS <= time.Now().UnixMilli()) {
		return checkinLogin()
	}
	spec, _ := SpecOf(id)
	s := spec.Quota
	s.Method, s.Body = "POST", "{}"
	switch id {
	case "astudio":
		return claimAstudio(ctx, a, day)
	case "autoclaw", "autoclawintl", "magicrew", "magicrewintl":
		return claimNewReward(ctx, id, a, day)
	case "loomy":
		s.Path = "/points/first-login"
	case "minimax", "minimaxintl":
		s.Path = "/minimax-cloud/api/v1/signin/claim?timezone_id=Asia%2FShanghai"
	case "buddy":
		s.Path = "/v2/billing/meter/daily-checkin"
	case "codearts":
		return claimCodeArts(ctx, s, a, day)
	case "lobsterai":
		return claimLobster(ctx, s, a, day)
	case "trae":
		return claimTrae(ctx, s, a, day)
	}
	p, status, failure := claimRequest(ctx, s, a)
	if failure != nil {
		return *failure
	}
	code := checkinCode(p, "code")
	if id == "buddy" {
		if (status == 200 || status == 400) && (code == "10001" || code == "1001") {
			return checkinDone("already", day)
		}
		if code == "1002" || code == "1003" {
			return checkin.Outcome{Status: "inactive", Message: "当前不在签到活动范围"}
		}
	}
	if status < 200 || status >= 300 {
		return checkinFailure(fmt.Sprintf("领取端点返回 HTTP %d", status))
	}
	d := objField(p, "data")
	switch id {
	case "minimax", "minimaxintl":
		if checkinCode(objField(p, "base_resp"), "status_code") != "0" {
			return checkinFailure("上游未确认签到成功（base_resp.status_code）")
		}
		switch checkinCode(d, "claim_result") {
		case "1":
			out := checkinDone("claimed", day)
			out.Amount = claimAmount(d, "points")
			return out
		case "2":
			return checkinDone("already", day)
		default:
			return checkinFailure("上游签到结果缺少有效的 claim_result")
		}
	case "loomy":
		if code == "100002" {
			return checkinLogin()
		}
		if code != "000000" || d == nil {
			return checkinFailure("上游未确认每日额度初始化成功")
		}
		// A stale dailyCycleDate must never advance today's watermark.
		coveredDay := field(d, "dailyCycleDate")
		if coveredDay == "" {
			coveredDay = day
		}
		if processed, ok := d["alreadyProcessed"].(bool); ok {
			if processed {
				return checkinDone("already", coveredDay)
			}
			if _, ok := numeric(d["dailyQuota"]); ok {
				out := checkinDone("claimed", coveredDay)
				out.Message = "上游已初始化每日赠送池；消耗后不回补"
				return out
			}
		}
		return checkinFailure("每日额度响应缺少领取判据，未记为成功")
	case "buddy":
		if code != "0" || d == nil {
			return checkinFailure("上游未确认每日领取成功")
		}
		out := checkinDone("claimed", day)
		out.Amount = claimAmount(d, "credit")
		return out
	}
	return checkinFailure("未实现的领取响应")
}

func claimAmount(data map[string]any, keys ...string) *float64 {
	for _, key := range keys {
		if n, ok := numeric(data[key]); ok && n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0) {
			return &n
		}
	}
	return nil
}

func claimCodeArts(ctx context.Context, s QuotaSpec, a Account, day string) checkin.Outcome {
	s.Method, s.Path, s.Body = "GET", "/v1/ops/delivery?channel=IDE", ""
	p, status, failure := claimRequest(ctx, s, a)
	if failure != nil {
		return *failure
	}
	if status != 200 || (p["code"] != nil && checkinCode(p, "code") != "0") {
		return checkinFailure("每日活动列表读取失败")
	}
	delivery := p
	if p["code"] != nil {
		delivery = objField(p, "data")
	}
	items, ok := delivery["items"].([]any)
	if !ok {
		return checkinFailure("每日活动响应缺少 items")
	}
	for _, item := range items {
		activity, _ := item.(map[string]any)
		if field(activity, "type") != "USER_LOGIN" {
			continue
		}
		if activity["claimable"] != true {
			switch field(activity, "status") {
			case "CLAIMED", "CONFIRMED", "CONSUMED":
				return checkinDone("already", day)
			}
			return checkin.Outcome{Status: "inactive", Message: "每日活动当前不可领取"}
		}
		campaign := checkinCode(activity, "campaignId")
		if campaign == "" {
			return checkinFailure("每日活动缺少 campaignId")
		}
		body, _ := json.Marshal(map[string]string{"campaignId": campaign, "channel": "IDE"})
		s.Method, s.Path, s.Body = "POST", "/v1/ops/claim", string(body)
		p, status, failure = claimRequest(ctx, s, a)
		if failure != nil {
			return *failure
		}
		if status == 200 && checkinCode(p, "code") == "40001" {
			return checkinDone("already", day)
		}
		data := objField(p, "data")
		valid := checkinCode(p, "code") == "0" && data != nil
		if p["code"] == nil && (p["status"] == "CLAIMED" || p["status"] == "CONFIRMED") {
			data, valid = p, true
		}
		if status != 200 || !valid {
			return checkinFailure("上游未确认每日权益领取成功")
		}
		out := checkinDone("claimed", day)
		out.Amount = claimAmount(data, "benefitAmount", "credit", "credits", "creditAmount")
		if data["id"] != nil {
			body, _ = json.Marshal(map[string]string{"campaignId": campaign})
			s.Path, s.Body = "/v1/ops/confirm", string(body)
			confirm, code, failure := claimRequest(ctx, s, a)
			confirmed := checkinCode(confirm, "code") == "0" || (confirm["code"] == nil && confirm["status"] == "CONFIRMED")
			if failure != nil || code != 200 || !confirmed {
				out.Status = "manual"
				out.Message = "权益已领取，确认步骤未完成，请在官方客户端核实后补签"
				out.CoveredDay = ""
			}
		}
		return out
	}
	return checkin.Outcome{Status: "inactive", Message: "未找到每日签到活动"}
}

func claimTrae(ctx context.Context, s QuotaSpec, a Account, day string) checkin.Outcome {
	if a.Field("uid") == "" {
		return checkinLogin()
	}
	s.Path = "/trae/api/v2/ug/checkin_credits/status"
	p, status, failure := claimRequest(ctx, s, a)
	if failure != nil {
		return *failure
	}
	if status != 200 || checkinCode(p, "code") != "0" {
		return checkinFailure("签到状态读取失败")
	}
	if p["checked_in"] == true {
		return checkinDone("already", day)
	}
	if p["enable"] != true {
		return checkin.Outcome{Status: "inactive", Message: "签到活动未开放"}
	}
	s.Path = "/trae/api/v2/ug/checkin_credits/claim"
	p, status, failure = claimRequest(ctx, s, a)
	if failure != nil {
		return *failure
	}
	if status != 200 || checkinCode(p, "code") != "0" {
		return checkinFailure("上游未确认签到成功，请稍后补签")
	}
	// Claim response has no amount. Do not invent a +0 reward.
	return checkinDone("claimed", day)
}

func claimLobster(ctx context.Context, s QuotaSpec, a Account, day string) checkin.Outcome {
	version := "2026.9.4" // Last verified official version; no invented version.
	update, status, failure := checkinRequest(ctx, "GET", "https://api-overmind.youdao.com/openapi/get/luna/hardware/lobsterai/prod/update", nil, "")
	if failure == nil && status == 200 {
		if remote := field(objField(objField(update, "data"), "value"), "version"); validLobsterVersion(remote) {
			version = remote
		}
	}
	s.Method, s.Path, s.Body = "GET", "/api/client-activities/slot?placement=desktop_sidebar&clientVersion="+url.QueryEscape(version)+"&containerApiVersion=2&platform=win32", ""
	request := func() (map[string]any, *checkin.Outcome) {
		p, status, failure := claimRequest(ctx, s, a)
		if failure != nil {
			return nil, failure
		}
		data := objField(p, "data")
		if status != 200 || checkinCode(p, "code") != "0" || data == nil {
			out := checkinFailure("签到活动响应未通过业务校验")
			return nil, &out
		}
		return data, nil
	}
	slot, failure := request()
	if failure != nil {
		return *failure
	}
	activity := objField(slot, "activity")
	code := field(activity, "activityCode")
	if slot["slotState"] != "available" || code == "" {
		return checkin.Outcome{Status: "inactive", Message: "当前没有可用签到活动"}
	}
	revision, ok := numeric(activity["configRevision"])
	if !ok {
		return checkinFailure("签到活动缺少版本号")
	}
	base := "/api/client-activities/" + url.PathEscape(code)
	s.Path = base + "/context?configRevision=" + strconv.FormatFloat(revision, 'f', -1, 64)
	data, failure := request()
	if failure != nil {
		return *failure
	}
	if objField(data, "state")["claimedToday"] == true {
		return checkinDone("already", day)
	}
	allowed := false
	if actions, ok := data["actions"].([]any); ok {
		for _, action := range actions {
			if value, ok := action.(string); ok && value == "check_in" {
				allowed = true
			}
		}
	}
	if !allowed {
		return checkin.Outcome{Status: "inactive", Message: "活动存在，但当前不可签到"}
	}
	body, _ := json.Marshal(map[string]any{"configRevision": revision, "idempotencyKey": uuidV4(), "payload": map[string]any{}})
	s.Method, s.Path, s.Body = "POST", base+"/actions/check_in", string(body)
	data, failure = request()
	if failure != nil {
		return *failure
	}
	out := checkinDone("claimed", day)
	out.Amount = claimAmount(objField(data, "result"), "creditsGranted", "rewardCredits", "credits")
	return out
}

func validLobsterVersion(version string) bool {
	if len(version) == 0 || len(version) > 32 {
		return false
	}
	for _, c := range version {
		if (c < '0' || c > '9') && c != '.' && c != '-' {
			return false
		}
	}
	return true
}
