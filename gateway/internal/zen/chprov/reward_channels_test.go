package chprov

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewDailyClaimsRequireExplicitBusinessResult(t *testing.T) {
	for _, tc := range []struct{ id, body, want string }{
		{"magicrew", `{"code":1000,"data":{"granted":true,"point_amount":300}}`, "claimed"},
		{"magicrewintl", `{"code":"1000","data":{"granted":false}}`, "already"},
		{"magicrew", `{"code":1000,"data":{}}`, "failed"},
		{"magicrew", `{"data":{"granted":true}}`, "failed"},
		{"magicrewintl", `{"code":2182,"data":{"granted":true}}`, "failed"},
		{"autoclaw", `{"code":0,"data":{"daily_check_in_completed_on_request":true}}`, "claimed"},
		{"autoclawintl", `{"code":0,"data":{"daily_check_in_completed_on_request":false}}`, "already"},
		{"autoclaw", `{"code":0,"data":{}}`, "failed"},
		{"autoclawintl", `{"code":410000}`, "login_required"},
	} {
		t.Run(tc.id+tc.body, func(t *testing.T) {
			checkinTestClient(t, func(r *http.Request) (int, string) {
				b, _ := io.ReadAll(r.Body)
				if r.Method != "POST" {
					t.Fatal(r.Method)
				}
				if strings.HasPrefix(tc.id, "magicrew") {
					if len(b) != 0 || r.URL.Path != "/api/v1/login-gift" || r.Header.Get("Authorization") != "Bearer management-jwt" || r.Header.Get("organization-code") != "org-one" {
						t.Fatal("wrong login gift request")
					}
				} else {
					if r.URL.Path != "/userapi/v1/user-info" || r.Header.Get("X-Authorization") != "Bearer management-jwt" || r.Header.Get("X-Auth-Sign") == "" || !strings.Contains(string(b), `"device_id":"stable-device"`) {
						t.Fatal("wrong AutoClaw claim identity")
					}
				}
				return 200, tc.body
			})
			out := ClaimDaily(context.Background(), tc.id, Account{AccessToken: "management-jwt", Extra: map[string]string{"organization_code": "org-one", "device_id": "stable-device"}}, "2026-11-06")
			if out.Status != tc.want {
				t.Fatalf("got %+v", out)
			}
			if strings.HasPrefix(tc.id, "autoclaw") && out.Amount != nil {
				t.Fatal("fabricated claim amount")
			}
			if tc.want == "claimed" || tc.want == "already" {
				if out.CoveredDay != "2026-11-06" {
					t.Fatal("lost business date")
				}
			} else if out.CoveredDay != "" {
				t.Fatal("failure marked today done")
			}
		})
	}
}

func TestAstudioClaimUsesOnlyDailyPopupAndControlCookie(t *testing.T) {
	calls := 0
	checkinTestClient(t, func(r *http.Request) (int, string) {
		calls++
		if r.Header.Get("Authorization") != "" || !strings.Contains(r.Header.Get("Cookie"), "ssoSessionId=session-test") || r.Header.Get("clientType") != "21" {
			t.Fatal("management/inference identities mixed")
		}
		if calls == 1 {
			return 200, `{"code":0,"flag":true,"data":[{"componentType":"NEW_USER_DIALOG","popupId":1},{"componentType":"DAILY_REWARD_DIALOG","popupId":3,"instanceKey":"today","content":"{\"points\":120}"}]}`
		}
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/xingchen-studio/client-popups/complete" || string(b) != `{"instanceKey":"today","popupId":3}` {
			t.Fatalf("wrong completion: %s", b)
		}
		return 200, `{"code":0,"data":{"points":120}}`
	})
	out := ClaimDaily(context.Background(), "astudio", Account{AccessToken: "inference-only", Extra: map[string]string{"sso_session_id": "session-test"}}, "2026-11-06")
	if out.Status != "claimed" || out.Amount == nil || *out.Amount != 120 || calls != 2 {
		t.Fatalf("%+v calls=%d", out, calls)
	}
}

func TestAstudioRejectsMissingOrMalformedPopupList(t *testing.T) {
	for _, body := range []string{`{"code":0}`, `{"code":0,"data":{}}`, `{"data":[]}`, `{"code":80000,"data":[]}`, `{"code":0,"data":[{"componentType":"DAILY_REWARD_DIALOG"}]}`} {
		checkinTestClient(t, func(*http.Request) (int, string) { return 200, body })
		out := ClaimDaily(context.Background(), "astudio", Account{AccessToken: "fake", Extra: map[string]string{"sso_session_id": "session"}}, "2026-11-06")
		if out.CoveredDay != "" || out.Status == "already" {
			t.Fatalf("malformed became success: %+v", out)
		}
	}
}

func TestNewQuotaDoesNotInventZeroOrIncludeSparkPool(t *testing.T) {
	for _, tc := range []struct {
		id, body       string
		failed, usable bool
	}{
		{"astudio", `{"code":0,"data":{"sparkTotalBalance":5000}}`, true, false},
		{"astudio", `{"code":0,"data":{"totalBalance":0,"sparkTotalBalance":5000}}`, false, false},
		{"astudio", `{"code":0,"data":{"totalBalance":1192,"sparkTotalBalance":5000}}`, false, true},
		{"magicrew", `{"code":1000,"data":{"total_points":"NaN"}}`, true, false},
		{"magicrew", `{"code":1000,"data":{"total_points":-3}}`, false, false},
		{"autoclaw", `{"code":0,"data":{}}`, true, false},
		{"autoclawintl", `{"code":0,"data":{"total_balance":50}}`, false, true},
	} {
		checkinTestClient(t, func(*http.Request) (int, string) { return 200, tc.body })
		q := ReadQuota(context.Background(), tc.id, Account{AccessToken: "fake", Extra: map[string]string{"sso_session_id": "fake"}})
		if q.Failed != tc.failed || (q.Known || q.Never) != tc.usable {
			t.Fatalf("%s: %+v", tc.id, q)
		}
	}
}

func TestNativeCatalogKeepsPaidModelsAndTextTokenMultiplier(t *testing.T) {
	checkinTestClient(t, func(r *http.Request) (int, string) {
		if r.URL.Path == "/api/v1/flows/models" {
			return 200, `{"code":1000,"data":{"models":[{"value":"free","label":"Free"},{"value":"paid","label":"GLM 5.3","vision":true}]}}`
		}
		if r.URL.Path != "/api/v1/products/model-points" {
			t.Fatal("unexpected pricing path")
		}
		return 200, `{"code":1000,"data":[{"model_id":"free","input_points":0},{"model_id":"paid","billing_type":"Images","input_points":30},{"model_id":"paid","billing_type":"TextTokens","input_points":"4000"}]}`
	})
	models, err := NativeCatalog(context.Background(), "magicrew", Account{AccessToken: "jwt"})
	if err != nil || len(models) != 2 || !models[1].Vision || models[1].Pricing.Rate == nil || *models[1].Pricing.Rate != 2 || !models[0].Pricing.ConfirmedFree() {
		t.Fatalf("models=%+v err=%v", models, err)
	}
}

func TestNativeAutoclawCatalogAcceptsBareListWithoutRelaxingClaims(t *testing.T) {
	checkinTestClient(t, func(r *http.Request) (int, string) {
		if r.Header.Get("Authorization") != "Bearer demo" || r.Header.Get("X-Authorization") != "" {
			t.Fatal("wrong catalog identity")
		}
		return 200, `[{"model":"paid-model","name":"Paid model","maxOutputTokens":4096,"inputModalities":["text","IMAGE"]}]`
	})
	ctx := context.Background()
	models, err := NativeCatalog(ctx, "autoclaw", Account{AccessToken: "demo"})
	if err != nil || len(models) != 1 || models[0].ID != "paid-model" || models[0].MaxOutput != 4096 || !models[0].Vision {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	if _, _, failure := checkinRequest(ctx, "GET", "https://example.invalid/claim", map[string]string{"Authorization": "Bearer demo"}, ""); failure == nil {
		t.Fatal("bare list accepted as a claim response")
	}
}

func TestAstudioCatalogDecodesSlugAndNeverSendsCookieToInference(t *testing.T) {
	slug := base64.RawURLEncoding.EncodeToString([]byte("glm-test"))
	checkinTestClient(t, func(r *http.Request) (int, string) {
		if r.Host != "astronstudio-api-volces-prod.xf-yun.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "Bearer model-only" {
			t.Fatal("wrong model authentication")
		}
		return 200, `{"code":0,"data":{"models":[{"slug":"builtin/` + slug + `","display_name":"GLM Test","input_modalities":["text","image"]},{"slug":"hidden","visibility":"hide"}]}}`
	})
	models, err := NativeCatalog(context.Background(), "astudio", Account{AccessToken: "model-only", Extra: map[string]string{"sso_session_id": "never-send"}})
	if err != nil || len(models) != 1 || models[0].ID != "glm-test" || !models[0].Vision {
		t.Fatalf("%+v %v", models, err)
	}
}

func TestNewChannelImportPreservesDistinctIdentityFields(t *testing.T) {
	a, why := accountFromPluginJSON("astudio", `{"access_token":"model","uid":"user-one","account_id":"billing-one","sso_session_id":"session"}`)
	if why != "" || a.ID != "user-one" || a.Field("account_id") != "billing-one" {
		t.Fatal("AStudio UID incorrectly replaced account ID")
	}
	m, why := accountFromPluginJSON("magicrewintl", `{"jwt":"management","apiKey":"inference","organizationCode":"organization"}`)
	if why != "" || m.AccessToken != "management" || m.Field("api_key") != "inference" || m.Field("organization_code") != "organization" {
		t.Fatal("MagiCrew credentials confused")
	}
}

func TestNativeKeyIssuanceAndAutoclawRefreshUseOwnRegion(t *testing.T) {
	checkinTestClient(t, func(r *http.Request) (int, string) {
		if r.Host == "magic-service.magicrew.ai" {
			if r.URL.Path != "/api/v1/model-gateway/tokens" || r.Header.Get("Authorization") != "Bearer jwt" || r.Header.Get("organization-code") != "org" {
				t.Fatal("wrong gateway key authorization")
			}
			return 200, `{"code":1000,"data":{"api_key":"new-inference","api_key_expires_at":1999999999000}}`
		}
		if r.Host != "autoglm-api.autoglm.ai" || r.URL.Path != "/userapi/v1/refresh" {
			t.Fatal("refresh went to wrong region")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["refresh_token"] != "refresh" || body["source_id"] != "autoclaw" {
			t.Fatal("wrong refresh body")
		}
		return 200, `{"code":0,"data":{"access_token":"fresh","refresh_token":"rotated","expires_in":3600}}`
	})
	original := Account{AccessToken: "jwt", Extra: map[string]string{"organization_code": "org"}}
	fresh, err := PrepareNativeAccount(context.Background(), "magicrewintl", original)
	if err != nil || fresh.Field("api_key") != "new-inference" || original.Field("api_key") != "" {
		t.Fatal("issuance failed or mutated original snapshot")
	}
	fresh, failure := CheckinAccount(context.Background(), "autoclawintl", Account{AccessToken: "old", RefreshTok: "refresh", ExpiresAtMS: 1})
	if failure != nil || fresh.AccessToken != "fresh" || fresh.RefreshTok != "rotated" || fresh.ExpiresAtMS <= time.Now().UnixMilli() {
		t.Fatal("AutoClaw refresh failed")
	}
}
