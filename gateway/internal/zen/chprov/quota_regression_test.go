package chprov

import (
	"fmt"
	"testing"
	"time"
)

func TestQuotaRemainingAndExpiryBelongToSameBucket(t *testing.T) {
	future := time.Now().Add(48 * time.Hour).UnixMilli()
	past := time.Now().Add(-time.Hour).UnixMilli()
	cases := []struct {
		name, channel, body, remaining string
		known, never                   bool
	}{
		{"trae consumed limit", "trae", fmt.Sprintf(`{"user_entitlement_pack_list":[{"expire_time":%d,"entitlement_base_info":{"quota":{"credits_limit":100}},"usage":{"credits_amount":100}}]}`, future), "", false, false},
		{"trae real remainder", "trae", fmt.Sprintf(`{"data":{"user_entitlement_pack_list":[{"expire_time":%d,"entitlement_base_info":{"quota":{"credits_limit":100}},"usage":{"credits_amount":80}}]}}`, future), "20 积分", true, false},
		{"trae missing usage", "trae", `{"user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":100}}}]}`, "", false, false},
		{"office no duplicate total", "officeace", fmt.Sprintf(`{"totalCredits":100,"creditGroups":[{"remaining":0,"expiresAt":%d},{"remaining":100,"expiresAt":%d}]}`, past, future), "100 积分", true, false},
		{"office raw subscription", "officeace", fmt.Sprintf(`{"skus":[{"sku_code":"plan","quotas":[{"sku_attr_code":"officeace_points","sku_value":"100","current_value":"80","expired_time":%d}]}]}`, future), "20 积分", true, false},
		{"codearts no gift expiry", "codearts", fmt.Sprintf(`{"end_date":%d,"metrics":[{"name":"usageBasicPackageCredit","package_credit_remain":0},{"name":"usageBonusPackageCredit","package_credit_remain":50},{"name":"usageTotalPackageCredit","package_credit_remain":50}]}`, future), "50 积分", false, true},
		{"qoder addons still usable", "qoder", fmt.Sprintf(`{"data":{"userQuota":{"remaining":0},"addOnQuota":{"remaining":100},"expiresAt":%d}}`, past), "100", false, true},
		{"qoder empty plan", "qoder", fmt.Sprintf(`{"userQuota":{"remaining":0},"expiresAt":%d}`, future), "", false, false},
		{"qoder legacy sidecar", "qoder", fmt.Sprintf(`{"data":{"remaining":20,"reset_at_ms":%d}}`, future), "20", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := ParseQuota(tc.channel, []byte(tc.body))
			if q.Failed || q.Known != tc.known || q.Never != tc.never || q.Remaining != tc.remaining {
				t.Fatalf("quota = %+v", q)
			}
		})
	}
}

func TestRegressionZeroBuddyCreditsAreUnusable(t *testing.T) {
	payload := map[string]any{"code": float64(0), "data": map[string]any{"Response": map[string]any{"Data": map[string]any{"Accounts": []any{map[string]any{"Status": float64(0), "CycleCapacityRemain": float64(0), "DeductionEndTime": float64(time.Now().Add(time.Hour).UnixMilli())}}}}}}
	q := buddyQuota(Spec{}, Account{}, payload)
	if q.Known || q.Never {
		t.Fatalf("zero remaining credits treated as usable: known=%v never=%v", q.Known, q.Never)
	}
}
func TestRegressionZeroZcodeCreditsAreUnusable(t *testing.T) {
	payload := map[string]any{"code": float64(0), "data": map[string]any{"balances": []any{map[string]any{"available_units": float64(0), "expires_at": float64(time.Now().Add(time.Hour).Unix())}}}}
	q := shapeHandlers["zcode"](Spec{}, Account{}, payload)
	if q.Known || q.Never {
		t.Fatalf("zero remaining credits treated as usable: known=%v never=%v", q.Known, q.Never)
	}
}
