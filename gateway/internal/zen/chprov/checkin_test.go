package chprov

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type checkinRT func(*http.Request) (*http.Response, error)

func (f checkinRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func checkinTestClient(t *testing.T, fn func(*http.Request) (int, string)) {
	t.Helper()
	old := Client
	t.Cleanup(func() { Client = old })
	Client = &http.Client{Transport: checkinRT(func(r *http.Request) (*http.Response, error) {
		code, body := fn(r)
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})}
}

func TestClaimDailyBusinessResults(t *testing.T) {
	for _, tc := range []struct {
		id, body, status string
		http             int
	}{
		{"minimax", `{"base_resp":{"status_code":0},"data":{"claim_result":1,"points":800}}`, "claimed", 200},
		{"minimaxintl", `{"base_resp":{"status_code":0},"data":{"claim_result":2}}`, "already", 200},
		{"minimax", `{"base_resp":{"status_code":1004},"data":{"claim_result":1}}`, "failed", 200},
		{"minimax", `{"data":{"claim_result":1}}`, "failed", 200},
		{"minimax", `{"base_resp":{"status_code":0},"data":{}}`, "failed", 200},
		{"buddy", `{"code":10001}`, "already", 400},
		{"buddy", `{"code":0,"data":{"credit":100}}`, "claimed", 200},
		{"buddy", `{"code":0}`, "failed", 200},
		{"buddy", `{"code":1002}`, "inactive", 200},
		{"loomy", `{"code":"000000","data":{"alreadyProcessed":true}}`, "already", 200},
		{"loomy", `{"code":"000000","data":{"alreadyProcessed":false,"dailyQuota":5000,"dailyConsumed":123}}`, "claimed", 200},
		{"loomy", `{"code":"000000","data":{}}`, "failed", 200},
		{"loomy", `{"code":"100002"}`, "login_required", 200},
		{"buddy", `<html>secret-token</html>`, "failed", 502},
		{"buddy", `{"secret":"secret-token"}`, "login_required", 401},
		{"buddy", `{"code":0,"data":{}}`, "failed", 500},
	} {
		t.Run(tc.id+tc.status+tc.body, func(t *testing.T) {
			checkinTestClient(t, func(r *http.Request) (int, string) {
				if r.Method != "POST" {
					t.Fatal(r.Method)
				}
				if strings.HasPrefix(tc.id, "minimax") && r.URL.Query().Get("timezone_id") != "Asia/Shanghai" {
					t.Fatal("missing IANA business timezone")
				}
				if tc.id == "loomy" && (r.Header.Get("token") != "fake-test-token" || r.Header.Get("Authorization") != "") {
					t.Fatal("Loomy business token header was replaced by account signature")
				}
				return tc.http, tc.body
			})
			out := ClaimDaily(context.Background(), tc.id, Account{AccessToken: "fake-test-token"}, "2026-11-06")
			if out.Status != tc.status {
				t.Fatalf("got %+v", out)
			}
			if strings.Contains(out.Message, "secret-token") {
				t.Fatal("upstream secrets leaked")
			}
			if (out.Status == "claimed" || out.Status == "already") && out.CoveredDay != "2026-11-06" {
				t.Fatal("success lost business date")
			}
		})
	}
}

func TestUnsupportedAndExpiredClaimsNeverSend(t *testing.T) {
	calls := 0
	checkinTestClient(t, func(*http.Request) (int, string) { calls++; return 200, `{}` })
	for _, id := range []string{"workbuddy", "zcode", "qoder", "qodercn", "groq"} {
		_ = ClaimDaily(context.Background(), id, Account{AccessToken: "fake"}, "2026-11-06")
	}
	_ = ClaimDaily(context.Background(), "buddy", Account{AccessToken: "fake", ExpiresAtMS: 1}, "2026-11-06")
	if calls != 0 {
		t.Fatal("manual/unsupported/expired credentials sent network calls")
	}
}

func TestClaimRedirectDoesNotForwardCredentials(t *testing.T) {
	calls := 0
	old := Client
	t.Cleanup(func() { Client = old })
	Client = &http.Client{Transport: checkinRT(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 307, Header: http.Header{"Location": {"https://other.example/claim"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	out := ClaimDaily(context.Background(), "minimaxintl", Account{AccessToken: "fake"}, "2026-11-06")
	if calls != 1 || out.Status != "failed" {
		t.Fatal("followed credentialed redirect", calls, out)
	}
}

func TestCodeArtsSignsClaimBodyAndConfirms(t *testing.T) {
	steps := 0
	checkinTestClient(t, func(r *http.Request) (int, string) {
		steps++
		if r.Header.Get("Agent-Type") != "PromptCenter" || !strings.Contains(r.Header.Get("Authorization"), "SDK-HMAC-SHA256") {
			t.Fatal("missing Huawei auth/routing")
		}
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
		}
		sum := sha256.Sum256(body)
		if r.Header.Get("X-Sdk-Content-Sha256") != hex.EncodeToString(sum[:]) {
			t.Fatal("signed body differs from actual bytes")
		}
		switch r.URL.Path {
		case "/v1/ops/delivery":
			return 200, `{"code":0,"data":{"items":[{"type":"OTHER","claimable":true,"campaignId":2},{"type":"USER_LOGIN","claimable":true,"campaignId":1}]}}`
		case "/v1/ops/claim":
			if !strings.Contains(string(body), `"campaignId":"1"`) {
				t.Fatal(string(body))
			}
			return 200, `{"code":0,"data":{"id":"benefit","benefitAmount":1000}}`
		case "/v1/ops/confirm":
			return 200, `{"code":0,"data":{}}`
		default:
			t.Fatal(r.URL.Path)
			return 500, `{}`
		}
	})
	out := ClaimDaily(context.Background(), "codearts", Account{Extra: map[string]string{"access_key_id": "test-ak", "secret_access_key": "test-sk", "security_token": "test-st"}}, "2026-11-06")
	if steps != 3 || out.Status != "claimed" || out.Amount == nil || *out.Amount != 1000 {
		t.Fatal(steps, out)
	}
}

func TestLobsterActivityPrecheckAndIdempotency(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "claim", true: "already"}[claimed], func(t *testing.T) {
			writes := 0
			checkinTestClient(t, func(r *http.Request) (int, string) {
				switch {
				case r.URL.Host == "api-overmind.youdao.com":
					if r.Header.Get("Authorization") != "" {
						t.Fatal("credential sent to public version endpoint")
					}
					return 200, `{"code":0,"data":{"value":{"version":"2026.11.6"}}}`
				case r.URL.Path == "/api/client-activities/slot":
					if r.URL.Query().Get("clientVersion") != "2026.11.6" {
						t.Fatal("did not use current official client version")
					}
					return 200, `{"code":0,"data":{"slotState":"available","activity":{"activityCode":"daily","configRevision":3}}}`
				case strings.HasSuffix(r.URL.Path, "/context"):
					if claimed {
						return 200, `{"code":0,"data":{"state":{"claimedToday":true},"actions":["check_in"]}}`
					}
					return 200, `{"code":0,"data":{"state":{"claimedToday":false},"actions":["check_in"]}}`
				default:
					writes++
					body, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(body), "idempotencyKey") {
						t.Fatal("missing upstream idempotency key")
					}
					return 200, `{"code":0,"data":{"result":{"creditsGranted":42}}}`
				}
			})
			out := ClaimDaily(context.Background(), "lobsterai", Account{AccessToken: "fake"}, "2026-11-06")
			if claimed && (writes != 0 || out.Status != "already") {
				t.Fatal(writes, out)
			}
			if !claimed && (writes != 1 || out.Status != "claimed") {
				t.Fatal(writes, out)
			}
		})
	}
}

func TestMinimaxRefreshUsesCorrectRegionAndRotatesTokens(t *testing.T) {
	checkinTestClient(t, func(r *http.Request) (int, string) {
		if r.URL.Host != "account.minimax.io" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatal("wrong refresh origin/protocol")
		}
		_ = r.ParseForm()
		if r.Form.Get("refresh_token") != "fake-refresh" || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("scope") != "agent.default" || r.Form.Get("audience") != "agent-backend" {
			t.Fatal("missing OAuth refresh fields")
		}
		return 200, `{"access_token":"fake-next","refresh_token":"fake-rotated","expires_in":3600}`
	})
	a, out := CheckinAccount(context.Background(), "minimaxintl", Account{AccessToken: "fake-old", RefreshTok: "fake-refresh", ExpiresAtMS: 1})
	if out != nil || a.AccessToken != "fake-next" || a.RefreshTok != "fake-rotated" || a.ExpiresAtMS <= time.Now().UnixMilli() {
		t.Fatal(a.ExpiresAtMS, out)
	}
}

func TestCodeArtsBareBusinessResponsesAndConfirmationFailure(t *testing.T) {
	for _, confirmOK := range []bool{true, false} {
		t.Run(map[bool]string{true: "confirmed", false: "needs-manual"}[confirmOK], func(t *testing.T) {
			checkinTestClient(t, func(r *http.Request) (int, string) {
				switch r.URL.Path {
				case "/v1/ops/delivery":
					return 200, `{"items":[{"type":"USER_LOGIN","claimable":true,"campaignId":1}]}`
				case "/v1/ops/claim":
					return 200, `{"status":"CLAIMED","id":"benefit","benefitAmount":1000}`
				case "/v1/ops/confirm":
					if confirmOK {
						return 200, `{"status":"CONFIRMED"}`
					}
					return 503, `{}`
				}
				return 500, `{}`
			})
			out := ClaimDaily(context.Background(), "codearts", Account{Extra: map[string]string{"access_key_id": "test", "secret_access_key": "test"}}, "2026-11-06")
			if confirmOK && (out.Status != "claimed" || out.CoveredDay != "2026-11-06") {
				t.Fatal(out)
			}
			if !confirmOK && (out.Status != "manual" || out.CoveredDay != "") {
				t.Fatal("unconfirmed reward recorded as complete", out)
			}
		})
	}
}

func TestTraePrecheckAndStableAccountIdentity(t *testing.T) {
	for _, checked := range []bool{false, true} {
		t.Run(map[bool]string{false: "claim", true: "skip"}[checked], func(t *testing.T) {
			calls := 0
			checkinTestClient(t, func(r *http.Request) (int, string) {
				calls++
				if r.Header.Get("Authorization") != "Cloud-IDE-JWT fake" || r.Header.Get("X-Market-User-Id") == "" || r.Header.Get("X-Device-Id") == "" {
					t.Fatal("missing per-account identity")
				}
				if strings.HasSuffix(r.URL.Path, "/status") {
					if checked {
						return 200, `{"code":0,"checked_in":true,"enable":true}`
					}
					return 200, `{"code":0,"checked_in":false,"enable":true}`
				}
				return 200, `{"code":0,"message":"success"}`
			})
			out := ClaimDaily(context.Background(), "trae", Account{AccessToken: "fake", Extra: map[string]string{"uid": "test-uid"}}, "2026-11-06")
			if checked && (calls != 1 || out.Status != "already") {
				t.Fatal(calls, out)
			}
			if !checked && (calls != 2 || out.Status != "claimed" || out.Amount != nil) {
				t.Fatal("unknown reward amount fabricated", calls, out)
			}
		})
	}
}
