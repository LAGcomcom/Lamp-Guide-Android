package lane

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The WorkBuddy 账号渠道 (intl): a credential-bearing lane source modeled on
// the Kilo template. The upstream speaks an OpenAI Chat variant on
// /v2/chat/completions — SSE, delta.reasoning_content for thinking — so the
// shared chat decoder works as is; what this source adds is the account
// lifecycle: a state-polling OAuth login and a refresh flow whose refresh token
// ROTATES upstream (two concurrent refreshes void the second one — every
// refresh is serialized).
//
// The free models ride a 24h rolling window per account: upstream answers 429
// with code 6004 and a reset wall clock, 14003 (or its text) for momentary
// crowding, 402 when paid balance is gone. The three free ids
// (hy4-preview-f, hy3, deepseek-v4.1-flash) are shipped as a static catalog:
// the dynamic listing endpoint sits behind the WAF and flakes, and a roster
// this small is cheaper to curate than to fetch.
//
// Refusals come in THREE unrelated shapes and the old code filed all of them as
// "the content gate":
//
//	400 request illegal                 → the conversation header family is
//	                                      missing (intl); no prompt rewrite
//	                                      can satisfy it  → wbConversationHeaders
//	403 code 11128 (or 403 + "unapproved channel")
//	                                    → the request TEXT matched a blocked
//	                                      template → wbRewrites, retried once
//	403 code 11140 ("unusual activity") → the ACCOUNT is restricted upstream;
//	                                      nothing local lifts it, so park it
//	                                      → wbRestrictionWait
//
// Prompts may be logged by the upstream; the blurb says so in front of the
// user, same discipline as the Kilo pool.

// WorkBuddyBase is the intl gateway root; override for tests.
var WorkBuddyBase = "https://www.workbuddy.ai"

const (
	wbTurnTimeout = streamIdleTimeout // shares the lane-wide silence bound (PR #40)

	wbChatPath = "/v2/chat/completions"

	wbAuthStatePath    = "/v2/plugin/auth/state"
	wbAuthTokenPath    = "/v2/plugin/auth/token"
	wbAuthRefreshPath  = "/v2/plugin/auth/token/refresh"
	wbLoginAccountPath = "/v2/plugin/login/account"
	wbUserResourcePath = "/v2/billing/meter/get-user-resource"

	wbLoginPending   = 11217 // upstream code: the browser login is not finished yet
	wbLoginTTL       = 10 * time.Minute
	wbRefreshEarly   = 2 * time.Hour // refresh proactively this close to expiry
	wbSoftCooldown   = 60            // seconds; the soft-limit single-model window
	wbCapDefaultWait = 600           // seconds when 6004 carries no parsable reset clock
)

// Two internal sentinels, never escaping this file:
//
//	wbCodeSanitize  the request TEXT hit a blocked template — rewrite and retry once
//	wbCodeRestricted the ACCOUNT is restricted upstream — park it, never replay
const (
	wbCodeSanitize = "WB_SANITIZE"
	// wbCodeRestricted marks upstream code 11140. It is deliberately NOT a
	// CodeCredential: the token is fine (billing reads healthy), only the chat
	// plane is refusing this account.
	wbCodeRestricted = "WB_RESTRICTED"
)

// WBAccount is one WorkBuddy login. store.Config persists it verbatim; the
// lane keeps a snapshot and writes rotated token pairs back through the
// persist callback.
type WBAccount struct {
	ChannelKey       string `json:"-"` // Source account key survives token rotation in memory.
	AccessToken      string `json:"accessToken,omitempty"`
	RefreshToken     string `json:"refreshToken,omitempty"`
	UID              string `json:"uid,omitempty"`
	Nickname         string `json:"nickname,omitempty"`
	ExpiresAtMS      int64  `json:"expiresAtMs,omitempty"`
	Enabled          bool   `json:"enabled"`
	LastActivityDate string `json:"lastActivityDate,omitempty"`
	// Realm is which publishment issued this token: "intl" (empty) or "cn".
	// The header family travels as ONE group — a CN token presented with intl
	// Origin/X-Domain/UA is refused on shape alone.
	Realm string `json:"realm,omitempty"`
}

// wbUUIDv4Shape turns 32 raw hex into the hyphenated 8-4-4-4-12 form the
// official clients emit (OS hardware UUID / per-process randomUUID), with the
// version nibble forced to 4 and the variant nibble to an RFC-4122 value.
//
// Shape matters as much as stability: a bare 36-hex or a prefixed id is itself
// a gateway signature, because no official client ever emits one.
func wbUUIDv4Shape(h string) string {
	if len(h) < 32 {
		return h
	}
	h = h[:32]
	b := []byte(h)
	b[12] = '4'
	switch b[16] {
	case '8', '9', 'a', 'b':
	default:
		b[16] = 'a'
	}
	h = string(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// wbDeriveID derives the stable per-account device identity: md5("<salt>:<uid>")
// in UUIDv4 shape — never re-randomized, so one account always looks like one
// machine and different accounts never correlate to the same device.
func wbDeriveID(uid, salt string) string {
	sum := md5.Sum([]byte(salt + ":" + uid))
	return wbUUIDv4Shape(hex.EncodeToString(sum[:]))
}

// wbDrain reads a bounded error/JSON body off the pump. Unlike a plain
// io.ReadAll it keeps a per-read deadline: the upstream answers refusals over
// HTTP/2 and does not always half-close the stream, so "the body ended" never
// arrives and an unbounded drain parks the caller for the full idle timeout.
// Data that keeps flowing keeps resetting the deadline — only true silence
// ends the drain.
func wbDrain(s *streamReader) []byte {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 2048)
	for len(buf) < 1<<20 {
		k, err := s.ReadTimeout(tmp, wbDrainReadTimeout)
		buf = append(buf, tmp[:k]...)
		if err != nil || k == 0 {
			return buf
		}
	}
	return buf
}

const wbDrainReadTimeout = 2 * time.Second

func wbRequestID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

// wbJWTClaims reads sub/iss/exp out of an access token. exp wins over any
// expiresAt field the endpoints carry — it is the JWT's own word.
func wbJWTClaims(token string) (sub, iss string, expMS int64, ok bool) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return "", "", 0, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		if payload, err = base64.RawStdEncoding.DecodeString(parts[1]); err != nil {
			return "", "", 0, false
		}
	}
	var claims struct {
		Sub string `json:"sub"`
		Iss string `json:"iss"`
		Exp any    `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return "", "", 0, false
	}
	return claims.Sub, claims.Iss, wbExpToMS(claims.Exp), true
}

func wbExpToMS(v any) int64 {
	f, ok := toNum(v)
	if !ok || f <= 0 {
		return 0
	}
	if f < 1e12 {
		return int64(f) * 1000 // seconds
	}
	return int64(f)
}

func wbTokenNearExpiry(acct WBAccount) bool {
	if acct.ExpiresAtMS <= 0 {
		return false // unknown lifetime: let an upstream 401 drive the refresh
	}
	return time.Now().UnixMilli()+wbRefreshEarly.Milliseconds() >= acct.ExpiresAtMS
}

// buildWorkBuddyCatalog is the intl free roster, one entry per x0.00 model in
// the desktop app's own listing. hy4/hy3 only accept the high effort rung, and
// deepseek-v4.1-flash runs thinking forced — hence CanDisableThinking false on
// all three; the reasoning_effort field is still set per rung (see
// wbApplyModelQuirks) but the off rung is never offered.
func buildWorkBuddyCatalog() []ModelInfo {
	blurb := "WorkBuddy 账号免费模型，24h 窗口限量；提示词可能被上游记录，别交机密内容"
	return []ModelInfo{
		{ID: "hy4-preview-f", Name: "Hy4 Preview", Blurb: blurb,
			Wire: "chat", Channel: ChannelWorkBuddy,
			Reasoning: true, ContextWindow: 1000000, MaxOutput: 64000, CanDisableThinking: false},
		{ID: "hy3", Name: "Hy3", Blurb: blurb,
			Wire: "chat", Channel: ChannelWorkBuddy,
			Reasoning: true, ContextWindow: 192000, MaxOutput: 64000, CanDisableThinking: false},
		{ID: "deepseek-v4.1-flash", Name: "DeepSeek V4.1 Flash", Blurb: blurb,
			Wire: "chat", Channel: ChannelWorkBuddy,
			Vision: true, FileInput: true, Reasoning: true,
			ContextWindow: 1000000, MaxOutput: 128000, CanDisableThinking: false},
	}
}

// workBuddyHeaders assembles the identity layer every WorkBuddy call shares
// (login polling, billing, refresh): the web layer, the IDE triple, and the
// credential. No cookies, no signatures — the JWT is the credential.
//
// ⚠️ This is the BASE set, not the chat set. The chat plane additionally needs
// the conversation header family and must NOT carry the self-made headers this
// map still lists for the billing routes; wbChatHeaders does that split.
func workBuddyHeaders(acct WBAccount, requestID string) map[string]string {
	realm := acct.Realm
	return map[string]string{
		"content-type":        "application/json",
		"accept":              "application/json",
		"x-requested-with":    "XMLHttpRequest",
		"x-codebuddy-request": "1",
		"accept-language":     wbAcceptLanguage(realm),
		"x-agent-purpose":     "conversation",
		"x-ide-name":          "WorkBuddy",
		"x-ide-type":          "WorkBuddy",
		"x-ide-version":       wbIDEVersionFor(realm),
		"x-domain":            wbDomainFor(realm),
		"user-agent":          wbDesktopUAFor(realm),
		"x-user-id":           acct.UID,
		"authorization":       "Bearer " + acct.AccessToken,
		"x-request-id":        requestID,
	}
}

func wbDomainFor(realm string) string {
	if realm == WBRealmCN {
		return "copilot.tencent.com"
	}
	return "www.workbuddy.ai"
}

func wbAcceptLanguage(realm string) string {
	if realm == WBRealmCN {
		return "zh-CN"
	}
	return "en-US"
}

// wbApplyModelQuirks layers this source's body extensions onto a chat body:
// forced streaming with usage, and per-family thinking fields (deepseek wants
// the thinking switch plus its own effort spelling; the hy family only
// accepts "high").
func wbApplyModelQuirks(body map[string]any, model, effort string) {
	body["stream_options"] = map[string]any{"include_usage": true}
	switch {
	case strings.HasPrefix(model, "deepseek"):
		body["thinking"] = map[string]any{"type": "enabled"}
		body["reasoning_effort"] = wbDeepseekEffort(effort)
	case strings.HasPrefix(model, "hy"):
		body["reasoning_effort"] = "high"
	}
}

// wbDeepseekEffort maps the rung ladder onto the effort spelling deepseek
// accepts. Thinking stays on for every rung (forced reasoning), the rung only
// picks how hard it thinks.
func wbDeepseekEffort(effort string) string {
	switch effort {
	case "light":
		return "low"
	case "deep":
		return "high"
	default:
		return "medium"
	}
}

// wbBackfillReasoning satisfies an upstream quirk: with thinking enabled, every
// assistant message must carry reasoning_content (an empty string passes).
// ToChatMessages never emits the field, so the gateway adds it.
func wbBackfillReasoning(body map[string]any) {
	msgs, ok := body["messages"].([]map[string]any)
	if !ok {
		return
	}
	for _, m := range msgs {
		if m["role"] != RoleAssistant {
			continue
		}
		if _, has := m["reasoning_content"]; !has {
			m["reasoning_content"] = ""
		}
	}
}

// wbPostOnce performs one physical POST and decodes the stream, following the
// same sniff-and-replay discipline as the other post paths. Non-2xx answers go
// through the WorkBuddy-specific classifier.
//
// conversationID is the upstream-facing conversation identity: stable per
// downstream conversation so the upstream prompt cache keeps hitting, fresh per
// turn for probes and the daily activity claim (they have no next turn).
func wbPostOnce(ctx context.Context, body map[string]any, acct WBAccount, conversationID string, onData func(payload []byte) error) (*Usage, error) {
	// The gate refuses a turn whose first message is not a system prompt; the
	// rule is enforced here — the one place every WorkBuddy POST goes through —
	// so the hand-built probe and activity bodies obey it exactly like a chat
	// turn does.
	wbEnsureSystemFirst(body)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, &UpstreamError{Code: CodeTransport, Message: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, WorkBuddyBase+wbChatPath, bytes.NewReader(payload))
	if err != nil {
		return nil, &UpstreamError{Code: CodeTransport, Message: err.Error()}
	}
	requestID := wbMessageID()
	base := workBuddyHeaders(acct, requestID)
	for k, v := range wbChatHeaders(base, acct.Realm, conversationID, requestID, requestID) {
		req.Header.Set(k, v)
	}
	resp, err := laneHTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &UpstreamError{Code: CodeAborted, Message: "cancelled"}
		}
		return nil, &UpstreamError{Code: CodeTransport, Message: err.Error()}
	}
	defer resp.Body.Close()

	retryAfter := 0
	if ra := resp.Header.Get("retry-after"); ra != "" {
		if n, perr := strconv.Atoi(ra); perr == nil && n > 0 && n < 3600 {
			retryAfter = n
		}
	}

	pump := newStreamReader(ctx, resp.Body, wbTurnTimeout)
	defer pump.Close()
	head := readUpTo(pump, 4096, 20*time.Second)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rest := wbDrain(pump)
		return nil, classifyWorkBuddyFailure(resp.StatusCode, string(head)+string(rest), retryAfter)
	}

	switch sniffBody(head) {
	case "empty":
		return nil, &UpstreamError{Code: CodeEmpty, Message: "empty response body"}
	case "json", "unknown":
		rest := wbDrain(pump)
		full := string(head) + string(rest)
		var parsed any
		if jerr := json.Unmarshal([]byte(full), &parsed); jerr != nil {
			return nil, &UpstreamError{Code: CodeServer, Message: truncate(full, 300)}
		}
		if m, ok := parsed.(map[string]any); ok {
			if _, has := m["error"]; has {
				return nil, classifyWorkBuddyFailure(resp.StatusCode, full, retryAfter)
			}
		}
		if uerr := onData([]byte(full)); uerr != nil {
			return nil, uerr
		}
		if m, ok := parsed.(map[string]any); ok {
			return usageFromBody(m), nil
		}
		return nil, nil
	}

	pump.Unread(head)
	usage, err := readSSE(pump, onData)
	if err != nil {
		if ctx.Err() != nil {
			return usage, &UpstreamError{Code: CodeAborted, Message: "cancelled"}
		}
		if ue, ok := err.(*UpstreamError); ok {
			return usage, ue
		}
		return usage, &UpstreamError{Code: CodeTransport, Message: err.Error()}
	}
	return usage, nil
}

// PostWorkBuddyStreamed posts one turn, retrying a content-gate hit once with
// the blocked templates rewritten. A second hit surfaces as a plain client
// error — the request is the caller's own text, replaying it cannot help.
//
// Restricted accounts (11140) are NOT retried here: that is an account verdict,
// not a request one, and a replay counts as a repeat offence upstream.
func PostWorkBuddyStreamed(ctx context.Context, body map[string]any, acct WBAccount, conversationID string, onData func(payload []byte) error) (*Usage, error) {
	usage, err := wbPostOnce(ctx, body, acct, conversationID, onData)
	if err == nil {
		return usage, nil
	}
	ue := asUpstream(err)
	if ue == nil || ue.Code != wbCodeSanitize {
		return usage, err
	}
	usage2, err2 := wbPostOnce(ctx, wbSanitize(body), acct, conversationID, onData)
	if err2 == nil {
		return usage2, nil
	}
	ue2 := asUpstream(err2)
	if ue2 != nil && ue2.Code == wbCodeSanitize {
		return usage, &UpstreamError{Code: CodeClient,
			Message: "上游内容闸拦了请求里的固定模板句，逐句改写后仍被拒——换一个说法，或去掉相关系统提示"}
	}
	return usage, err2
}

// wbSanitizeRoles is the role scope the content gate is enforced on. The
// boundary matrix behind it: the same identity sentence passes from a user
// message and 400s from system / assistant history. A scrub that rewrote user
// content would desync the model's view from what is on disk; one that skipped
// assistant history would leave the agent's own echo armed, because hosts replay
// their system prompt into the conversation.
var wbSanitizeRoles = map[string]bool{
	"system":      true,
	"developer":   true, // OpenAI-newer clients speak this instead of system
	RoleAssistant: true,
}

// wbSanitize rewrites the blocked templates out of a request body.
//
// Two passes with different scopes, because they are two different defects:
//
//  1. Identity-scoped (system/developer/assistant): the template family the
//     upstream generalized into "self-introduction sentence + brand". These are
//     the phrases that repeat every turn of every session.
//  2. Body-wide: the `@`-mention escape. The upstream parses "@ + long sentence"
//     as a file reference and fails the whole request when it cannot resolve
//     the path — and that failure wears the same 11128 badge as a prompt match.
//
// Messages are deep-copied: the caller's body must stay intact for the recovery
// paths, and the retry must be able to start over from it.
func wbSanitize(body map[string]any) map[string]any {
	next := cloneMap(body)
	msgs, ok := body["messages"].([]map[string]any)
	if !ok {
		return next
	}
	copied := make([]map[string]any, len(msgs))
	for i, m := range msgs {
		cm := cloneMap(m)
		role, _ := cm["role"].(string)
		identityScoped := wbSanitizeRoles[strings.ToLower(strings.TrimSpace(role))]
		cm["content"] = wbSanitizeContent(cm["content"], identityScoped)
		copied[i] = cm
	}
	next["messages"] = copied
	return next
}

// wbSanitizeContent rewrites one message's content field. The wire allows three
// shapes for it (a plain string, a []any of text parts, or the same parts
// already typed), and each has to keep its own shape: getting this wrong
// silently drops a message's text.
func wbSanitizeContent(content any, identityScoped bool) any {
	scrub := func(text string) string {
		if identityScoped {
			text = wbRewriteText(text)
		}
		return wbEscapeMentions(text)
	}
	switch v := content.(type) {
	case string:
		return scrub(v)
	case []any:
		parts := make([]any, len(v))
		for j, p := range v {
			pm, ok := p.(map[string]any)
			if !ok {
				parts[j] = p
				continue
			}
			cp := cloneMap(pm)
			if t, ok := cp["text"].(string); ok {
				cp["text"] = scrub(t)
			}
			parts[j] = cp
		}
		return parts
	case []map[string]any:
		parts := make([]any, len(v))
		for j, p := range v {
			cp := cloneMap(p)
			if t, ok := cp["text"].(string); ok {
				cp["text"] = scrub(t)
			}
			parts[j] = cp
		}
		return parts
	}
	return content
}

// wbEnsureSystemFirst makes the turn open with a system message, leaving an
// existing leading system prompt untouched. The gate's second check is "first
// message is not a system prompt" — a plain OpenAI-shaped request starts with
// the user, which is exactly what it refuses.
const wbSystemPreamble = "You are WorkBuddy, an AI assistant for everyday office work."

func wbEnsureSystemFirst(body map[string]any) {
	msgs, _ := body["messages"].([]map[string]any)
	if len(msgs) > 0 {
		if role, _ := msgs[0]["role"].(string); strings.EqualFold(role, RoleSystem) {
			return
		}
	}
	lead := map[string]any{"role": RoleSystem, "content": wbSystemPreamble}
	body["messages"] = append([]map[string]any{lead}, msgs...)
}

// Classify codes this source carries beyond the shared vocabulary.
var (
	wbCapLimitCode  = 6004  // the free window is spent; the only code with a reset clock
	wbSoftLimitCode = 14003 // momentary crowding
	// wbSessionDeadCode means the upstream lost the session ("offline user
	// session not found"). It is usually a refresh race or a dropped
	// connection, not a revoked login, so it cools briefly rather than
	// escalating.
	wbSessionDeadCode = 12153
	// wbBadBodyCode is the upstream refusing a malformed body. The request is
	// the caller's fault: rotating accounts replays the identical malformed
	// body, so it must never be charged against an account.
	wbBadBodyCode = 11101
)

var (
	wbResetTimeRe = regexp.MustCompile(`(?:reset at|将在)\s*(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2})`)
	wbResetTZRe   = regexp.MustCompile(`UTC([+-])(\d{1,2})(?::?(\d{2}))?`)
	wbSoftLimitRe = regexp.MustCompile(`(?i)usage exceeds frequency limit|too many requests|请求过于频繁`)
	wbSessionRe   = regexp.MustCompile(`(?i)offline user session not found|session not found|session expired`)
	wbBadBodyRe   = regexp.MustCompile(`(?i)unmarshal chat params failed|invalid request body|malformed`)
	// wbIllegalRe is the shape an intl conversation-header fault arrives in:
	// HTTP 400 with "request illegal" and NO numeric code. It says nothing
	// about the credential or the prompt, and the only fix is to send the
	// conversation header family.
	wbIllegalRe = regexp.MustCompile(`(?i)request illegal`)
)

// classifyWorkBuddyFailure reads one of this gateway's refusals onto the shared
// vocabulary.
//
// The status alone misleads here, and so does the code: 6004 arrives as a 429
// but names a daily window (its reset wall clock is the honest Retry-After),
// 14003 is momentary crowding, 402 is paid balance, 11128 is a request-text
// verdict, and 11140 is an account verdict. Filing them all as "the content
// gate" — which this function used to do — left the caller unable to tell
// "rewrite your prompt" from "this account is burned".
func classifyWorkBuddyFailure(status int, payload string, retryAfterSec int) *UpstreamError {
	code := wbCodeFromPayload(payload)
	switch {
	case code == wbCapLimitCode:
		wait := wbResetWait(payload)
		if wait <= 0 {
			wait = wbCapDefaultWait
		}
		if retryAfterSec > 0 && retryAfterSec < wait {
			wait = retryAfterSec
		}
		return &UpstreamError{Code: CodeQuota, Message: truncate(payload, 300), RetryAfter: wait}
	case code == wbSoftLimitCode || wbSoftLimitRe.MatchString(payload):
		return &UpstreamError{Code: CodeQuota, Message: truncate(payload, 300), RetryAfter: wbSoftCooldown}
	case status == 402:
		return &UpstreamError{Code: CodeQuota,
			Message:    truncate(payload, 300) + "（WorkBuddy 余额耗尽，免费模型不受影响）",
			RetryAfter: wbSecondsUntil4AM()}
	case status == 401:
		// Unconditional: the front gateway answers a dead credential with an
		// openresty HTML page, and the HTML special case downstream would
		// misfile it as a request fault — exactly the one status that must
		// trigger the refresh-and-retry path.
		return &UpstreamError{Code: CodeCredential, Message: "WorkBuddy 登录态失效（上游 401）"}
	case code == 11140:
		// The ACCOUNT is restricted upstream ("账号存在异常行为" / "your account
		// has been restricted due to unusual activity"). Billing can still read
		// healthy while the chat plane refuses this account, so this must not
		// be filed as a credential fault — that would send the caller down a
		// refresh-and-retry path that cannot possibly help, and every replay
		// counts as one more strike against an account already flagged.
		return &UpstreamError{Code: wbCodeRestricted,
			Message:    "WorkBuddy 账号被上游限制（11140 异常活动），只能等上游解除，换号或稍后重试",
			RetryAfter: int(wbRestrictionWait(1).Seconds())}
	case code == 11128:
		// A verdict about the request TEXT: rewrite the blocked template and
		// replay once. Nothing about the credential or the account.
		return &UpstreamError{Code: wbCodeSanitize,
			Message: "上游内容闸拦截（11128）：请求里的固定模板句命中了黑名单"}
	case status == 400 && wbIllegalRe.MatchString(payload):
		// "request illegal" with no numeric code is the intl conversation-header
		// fault, not a prompt match — no rewrite can help. Filed as a client
		// error so it never burns the account, and named so the operator knows
		// where to look.
		return &UpstreamError{Code: CodeClient,
			Message: "上游拒绝了请求信封（request illegal）：会话头族没到位，与提示词无关"}
	case code == wbBadBodyCode || wbBadBodyRe.MatchString(payload):
		return &UpstreamError{Code: CodeClient,
			Message: "上游拒绝请求体（" + strconv.Itoa(code) + "）: " + truncate(payload, 200)}
	case code == wbSessionDeadCode || wbSessionRe.MatchString(payload):
		// Soft: one 12153 is routinely a refresh race or a dropped connection.
		return &UpstreamError{Code: CodeCredential,
			Message: "WorkBuddy 会话失效（" + strconv.Itoa(code) + "）"}
	}
	return ClassifyFailure(status, payload, retryAfterSec)
}

// wbCodeFromPayload digs the numeric error code out of either envelope shape
// the upstream uses: {"code":6004,...} and {"error":{"code":6004,...}}.
func wbCodeFromPayload(payload string) int {
	var shaped map[string]any
	if json.Unmarshal([]byte(payload), &shaped) != nil {
		return 0
	}
	if f, ok := toNum(shaped["code"]); ok {
		return int(f)
	}
	if errObj, ok := shaped["error"].(map[string]any); ok {
		if f, ok := toNum(errObj["code"]); ok {
			return int(f)
		}
	}
	return 0
}

// wbResetWait parses the reset wall clock out of a 6004 body — intl spells it
// "your usage will reset at 2026-09-19 18:29:03 UTC+8" — and returns seconds
// from now. 0 when nothing parsable is present.
func wbResetWait(payload string) int {
	m := wbResetTimeRe.FindStringSubmatch(payload)
	if m == nil {
		return 0
	}
	stamp := strings.Replace(m[1], "T", " ", 1)
	t, err := time.ParseInLocation("2006-01-02 15:04:05", stamp, wbResetZone(payload))
	if err != nil {
		return 0
	}
	wait := int(time.Until(t).Seconds())
	if wait < 0 {
		return 0
	}
	if wait > 48*3600 {
		wait = 48 * 3600
	}
	return wait
}

func wbResetZone(payload string) *time.Location {
	m := wbResetTZRe.FindStringSubmatch(payload)
	if m == nil {
		return time.Local
	}
	h, _ := strconv.Atoi(m[2])
	min := 0
	if m[3] != "" {
		min, _ = strconv.Atoi(m[3])
	}
	off := h*3600 + min*60
	if m[1] == "-" {
		off = -off
	}
	return time.FixedZone("wb", off)
}

func wbSecondsUntil4AM() int {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return int(next.Sub(now).Seconds())
}
