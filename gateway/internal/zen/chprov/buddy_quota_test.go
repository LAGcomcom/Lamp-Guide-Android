package chprov

import (
	"testing"
	"time"
)

// buddy 系的报文是**双层嵌套 + PascalCase**：data.Response.Data.Accounts[]，
// 包内 DeductionEndTime / CycleCapacityRemain。
//
// ⚠️ 这条断言是拿真实响应形状写的（不带真实凭据也能跑）：只下钻一层会拿到
// 一个没有 Accounts 的对象，然后一切判成不可用 —— 而响应其实是 200。
func TestBuddyQuotaParsesNestedAccounts(t *testing.T) {
	now := time.Now().UnixMilli()
	payload := map[string]any{
		"code": float64(0),
		"data": map[string]any{
			"Response": map[string]any{
				"Data": map[string]any{
					"Accounts": []any{
						// 失效包：Status 3 + 余额很大，**不得**并进总额
						map[string]any{"Status": float64(3), "CycleCapacityRemain": float64(500),
							"ExpiredTime": "2026-06-02 00:00:00", "PackageName": "已过期包"},
						// 有效包：带扣费截止时刻 → 这份额度的临期时刻
						map[string]any{"Status": float64(0), "CycleCapacityRemain": float64(100),
							"DeductionEndTime": float64(now + 3600*1000), "PackageName": "有效包"},
					},
				},
			},
		},
	}
	q := buddyQuota(Spec{}, Account{}, payload)
	if !q.Known {
		t.Fatalf("没有读到扣费截止时刻: %+v", q)
	}
	if q.ExpiryMS != now+3600*1000 {
		t.Fatalf("临期 = %d, want %d", q.ExpiryMS, now+3600*1000)
	}
	// 失效包的 500 分不能并进总额：实测上游某账号因此从 155.67 虚高到 655.67。
	if q.Remaining != "100 积分" {
		t.Fatalf("余额 = %q, want 100 积分（失效包不得计入）", q.Remaining)
	}
}

// 只有失效包时是「已用光」，不是「读不到」：这两件事的用户动作不同。
func TestBuddyQuotaAllExpiredIsUnusableWithReason(t *testing.T) {
	payload := map[string]any{
		"code": float64(0),
		"data": map[string]any{"Response": map[string]any{"Data": map[string]any{
			"Accounts": []any{map[string]any{"Status": float64(3), "CycleCapacityRemain": float64(10)}},
		}}},
	}
	q := buddyQuota(Spec{}, Account{}, payload)
	if q.Failed {
		t.Fatal("全部失效不该报成「读不到」")
	}
	if q.OK() {
		t.Fatal("全部失效不该判成可用")
	}
	if q.Detail == "" {
		t.Fatal("零值必须带原因：否则面板只显示「上游说不可用」，用户分不清是没钱还是 token 过期")
	}
}

// 结构没对上时要报「缺哪一层」，而不是笼统一句「不可用」。
func TestBuddyQuotaNamesTheMissingLayer(t *testing.T) {
	q := buddyQuota(Spec{}, Account{}, map[string]any{"code": float64(0), "data": map[string]any{}})
	if !q.Failed || q.Detail == "" {
		t.Fatalf("应当报成 Failed 并说明缺哪一层: %+v", q)
	}
}
