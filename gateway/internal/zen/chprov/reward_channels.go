package chprov

// Protocol reference: iJetLi/dsh-providers-hub (MIT), commit a5b3cd10dd.
// Management sessions and inference credentials deliberately stay separate.
import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"zenui/internal/zen/checkin"
)

func newRewardSpec(id string) Spec {
	s := Spec{ID: id, Tier: TierExpiry, Login: LoginSpec{Kind: LoginPaste}}
	s.Quota = QuotaSpec{Method: "GET", Auth: AuthCustom}
	switch id {
	case "astudio":
		s.Quota.BaseURL, s.Quota.Path = "https://agent.xfyun.cn/xingchen-studio", "/points/balance"
		s.ExtraKeys = []string{"sso_session_id", "account_id", "token"}
		s.Login.PasteLabel = "模型 Bearer Token"
		s.Login.PasteHint = "从本机插件导入，或填写自己的模型 Bearer Token 与 sso_session_id。额度、签到使用会话 Cookie，推理使用模型 Token；两者不能互换。"
	case "autoclaw", "autoclawintl":
		s.Tier = TierNever // Wallet totals do not establish remaining ledger batch expiry.
		s.Quota.BaseURL, s.Quota.Path = "https://autoglm-acceleration-api.zhipuai.cn", "/agent-assetmgr/api/v2/wallets?biz_app_id=autoclaw"
		if id == "autoclawintl" {
			s.Quota.BaseURL = "https://autoglm-api.autoglm.ai"
		}
		s.ExtraKeys = []string{"device_id", "refresh_token"}
		s.Login.PasteLabel = "Access Token"
		s.Login.PasteHint = "从本机插件导入，或填写本站自己的 Access Token。国内与国际账号独立保存；device_id 可留空，程序会按账号生成稳定标识。"
	case "magicrew", "magicrewintl":
		s.Quota.BaseURL, s.Quota.Path = "https://i-magic-service.letsmagic.cn", "/api/v1/quota/points/total"
		if id == "magicrewintl" {
			s.Quota.BaseURL = "https://magic-service.magicrew.ai"
		}
		s.ExtraKeys = []string{"organization_code"}
		s.Login.PasteLabel = "网页登录 JWT"
		s.Login.PasteHint = "从本机插件导入，或填写自己的网页登录 JWT 和组织码。国际站必须先在网页选中组织；推理密钥由此会话独立签发。"
	}
	s.Quota.Headers = func(a Account) map[string]string { return rewardHeaders(id, a) }
	s.Note = s.Login.PasteHint
	return s
}

func rewardHeaders(id string, a Account) map[string]string {
	h := map[string]string{"Accept": "application/json", "Content-Type": "application/json"}
	switch id {
	case "astudio":
		pairs := []string{}
		for _, p := range [][2]string{{"ssoSessionId", a.Field("sso_session_id")}, {"sso_sessionid", a.Field("sso_session_id")}, {"account_id", a.Field("account_id")}, {"token", firstNonEmpty(a.Field("token"), a.Field("sso_session_id"))}} {
			if p[1] != "" {
				pairs = append(pairs, p[0]+"="+p[1])
			}
		}
		h["Cookie"], h["clientType"], h["studioVersion"] = strings.Join(pairs, "; "), "21", "3.5.2"
	case "autoclaw", "autoclawintl":
		// Public desktop application signing constant, not an account credential.
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		sign := md5.Sum([]byte("100003&" + ts + "&38d2391985e2369a5fb8227d8e6cd5e5"))
		h["X-Auth-Appid"], h["X-Auth-TimeStamp"], h["X-Auth-Sign"] = "100003", ts, hex.EncodeToString(sign[:])
		h["Authorization"], h["X-Authorization"] = "Bearer "+cleanRewardToken(a.AccessToken), "Bearer "+cleanRewardToken(a.AccessToken)
		h["X-Lang"], h["X-Channel"], h["User-Agent"] = "zh-CN", "official", "AutoClaw/2.1.1 (win)"
	default:
		origin := "https://www.letsmagic.cn"
		if id == "magicrewintl" {
			origin = "https://www.magicrew.ai"
		}
		h["Authorization"], h["Origin"], h["Referer"] = "Bearer "+a.AccessToken, origin, origin+"/"
		h["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/141.0.0.0 Safari/537.36"
		if org := a.Field("organization_code"); org != "" {
			h["organization-code"] = org
		}
	}
	return h
}

func cleanRewardToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) >= 7 && strings.EqualFold(token[:7], "Bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	return token
}

func rewardDevice(a Account) string {
	if d := a.Field("device_id"); d != "" {
		return d
	}
	h := sha256.Sum256([]byte("lamp-guide/autoclaw/" + a.Key()))
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func rewardEnvelope(id string, p map[string]any, status int) *checkin.Outcome {
	code := checkinCode(p, "code")
	if id == "astudio" && (code == "80000" || code == "1002" || code == "1005") || strings.HasPrefix(id, "autoclaw") && code == "410000" {
		out := checkinLogin()
		return &out
	}
	want := "0"
	if strings.HasPrefix(id, "magicrew") {
		want = "1000"
	}
	if status < 200 || status >= 300 || code != want || p["flag"] == false {
		out := checkinFailure("上游业务结果未确认成功，请稍后重试")
		if code == "2182" {
			out.Message = "国际站组织未选中，请在网页选中组织后重新导入账号"
		}
		return &out
	}
	return nil
}

func newRewardQuota(s Spec, a Account, payload any) Quota {
	p, ok := payload.(map[string]any)
	if !ok {
		return Fail("额度响应不是对象")
	}
	if failure := rewardEnvelope(s.ID, p, 200); failure != nil {
		return Fail(failure.Message)
	}
	d := objField(p, "data")
	key := "total_points"
	if s.ID == "astudio" {
		key = "totalBalance"
	}
	if strings.HasPrefix(s.ID, "autoclaw") {
		key = "total_balance"
	}
	remaining, ok := numeric(d[key])
	if !ok || math.IsNaN(remaining) || math.IsInf(remaining, 0) {
		return Fail("额度响应缺少可解析的 " + key)
	}
	q := Quota{Remaining: strconv.FormatFloat(remaining, 'f', -1, 64) + " 积分"}
	if remaining <= 0 {
		q.Detail = "上游确认余额已用完"
		return q
	}
	q.Never, q.Detail = true, "余额可用，上游未提供可核实的余额到期时刻"
	if strings.HasPrefix(s.ID, "magicrew") {
		rows, _ := d["expiring_quota_details"].([]any)
		for _, row := range rows {
			r, _ := row.(map[string]any)
			n, valid := numeric(r["remaining_points"])
			if !valid || n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
				continue
			}
			t, err := time.ParseInLocation("2006-01-02 15:04:05", strings.ReplaceAll(field(r, "expires_at"), "T", " "), checkin.BusinessZone)
			if err == nil && t.After(time.Now()) && (!q.Known || t.UnixMilli() < q.ExpiryMS) {
				q.Known, q.Never, q.ExpiryMS, q.Detail = true, false, t.UnixMilli(), "限时积分按北京时间到期"
			}
		}
	}
	if s.ID == "astudio" {
		// Offset-less timestamps have no verified timezone; do not invent one.
		for _, pool := range []string{"member", "buy", "activity"} {
			n, valid := numeric(d[pool+"Balance"])
			t, err := time.Parse(time.RFC3339, field(d, pool+"NextExpireTime"))
			if valid && n > 0 && err == nil && t.After(time.Now()) && (!q.Known || t.UnixMilli() < q.ExpiryMS) {
				q.Known, q.Never, q.ExpiryMS, q.Detail = true, false, t.UnixMilli(), "主积分池的最早到期；星火独立池不计入主池"
			}
		}
	}
	return q
}

func claimNewReward(ctx context.Context, id string, a Account, day string) checkin.Outcome {
	s := MustSpec(id).Quota
	s.Method, s.Body = "POST", ""
	if strings.HasPrefix(id, "magicrew") {
		if id == "magicrewintl" && a.Field("organization_code") == "" {
			return checkin.Outcome{Status: "manual", Message: "请先在国际站网页选中组织，并重新导入组织码"}
		}
		s.Path = "/api/v1/login-gift" // Empty-string body is part of the contract.
	} else {
		s.Path = "/userapi/v1/user-info"
		b, _ := json.Marshal(map[string]string{"source_id": "autoclaw", "device_id": rewardDevice(a)})
		s.Body = string(b)
	}
	p, status, failure := claimRequest(ctx, s, a)
	if failure != nil {
		return *failure
	}
	if failure = rewardEnvelope(id, p, status); failure != nil {
		return *failure
	}
	d := objField(p, "data")
	flag, amountKey := "granted", "point_amount"
	if strings.HasPrefix(id, "autoclaw") {
		flag = "daily_check_in_completed_on_request"
	}
	granted, ok := d[flag].(bool)
	if !ok {
		return checkinFailure("签到响应缺少 " + flag + "，未将今日标记为已领")
	}
	if !granted {
		return checkinDone("already", day)
	}
	out := checkinDone("claimed", day)
	if strings.HasPrefix(id, "magicrew") {
		out.Amount = claimAmount(d, amountKey)
	} else {
		out.Message = "签到成功；上游未返回领取数量，请刷新余额查看"
	}
	return out
}

func claimAstudio(ctx context.Context, a Account, day string) checkin.Outcome {
	if a.Field("sso_session_id") == "" {
		return checkinLogin()
	}
	s := MustSpec("astudio").Quota
	s.Method, s.Path, s.Body = "GET", "/client-popups/pending", ""
	p, status, failure := claimRequest(ctx, s, a)
	if failure != nil {
		return *failure
	}
	if failure = rewardEnvelope("astudio", p, status); failure != nil {
		return *failure
	}
	rows, ok := p["data"].([]any)
	if !ok {
		return checkinFailure("每日弹窗响应缺少清单，未将今日标记为已领")
	}
	out, seen := checkinDone("already", day), map[string]bool{}
	var sum float64
	allAmounts := true
	for _, row := range rows {
		r, _ := row.(map[string]any)
		kind := firstNonEmpty(field(r, "componentType"), field(r, "type"))
		if kind != "DAILY_REWARD_DIALOG" {
			continue
		} // One-off onboarding is separate.
		id, valid := numeric(r["popupId"])
		if !valid || id < 0 || id != float64(int64(id)) {
			return checkinFailure("每日奖励缺少有效 popupId")
		}
		identity := field(r, "popupId") + "/" + field(r, "instanceKey")
		if seen[identity] {
			continue
		}
		seen[identity] = true
		b, _ := json.Marshal(map[string]any{"popupId": id, "instanceKey": field(r, "instanceKey")})
		s.Method, s.Path, s.Body = "POST", "/client-popups/complete", string(b)
		p, status, failure = claimRequest(ctx, s, a)
		if failure != nil {
			return *failure
		}
		if failure = rewardEnvelope("astudio", p, status); failure != nil {
			return *failure
		}
		out = checkinDone("claimed", day)
		amount := astudioPoints(p["data"])
		if amount == nil {
			amount = astudioPoints(r["payload"])
		}
		if amount == nil {
			amount = astudioPoints(r["content"])
		}
		if amount != nil {
			sum += *amount
		} else {
			allAmounts = false
		}
	}
	if out.Status == "claimed" && allAmounts {
		out.Amount = &sum
	}
	return out
}

func astudioPoints(v any) *float64 {
	if s, ok := v.(string); ok && strings.HasPrefix(strings.TrimSpace(s), "{") {
		var p any
		if json.Unmarshal([]byte(s), &p) == nil {
			return astudioPoints(p)
		}
	}
	if m, ok := v.(map[string]any); ok {
		return claimAmount(m, "points", "credit", "rewardPoints", "reward_points")
	}
	return claimAmount(map[string]any{"value": v}, "value")
}
