package modelprice

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRatesKeepZeroUnknownAndUnitsDistinct(t *testing.T) {
	for _, tc := range []struct {
		v     any
		want  float64
		known bool
	}{
		{"x0.29", .29, true}, {"0.50x", .5, true}, {"x0.03 credits", .03, true}, {"x0", 0, true},
		{0.0, 0, true}, {nil, 0, false}, {"", 0, false}, {"0.5", 0, false}, {"$0.5/M", 0, false}, {-1.0, 0, false},
	} {
		got := ParseRate(tc.v)
		if (got != nil) != tc.known || got != nil && *got != tc.want {
			t.Errorf("ParseRate(%v)=%v", tc.v, got)
		}
	}
	row := map[string]any{"pricing": map[string]any{"input": .5, "output": 1.0}}
	if Parse(row).Effective(time.Now()) != nil {
		t.Fatal("token pricing became a credit multiplier")
	}
}

func TestPromotionsFollowTimePriorityAndExpiry(t *testing.T) {
	var data map[string]any
	if err := json.Unmarshal([]byte(`{"modelPromotions":[
	 {"kind":"discount","enabled":true,"priority":1,"modelIds":["paid"],"discount":{"discountedCredits":"0.50x"},"schedule":{"daily":[{"start":"23:00","end":"07:50"}],"timezone":"Asia/Shanghai"}},
	 {"kind":"discount","enabled":true,"priority":10,"modelIds":["paid"],"discount":{"discountedCredits":"0x"},"schedule":{"validFrom":"2026-11-01T00:00:00Z","validUntil":"2026-11-06T00:00:00Z","daily":[{"start":"23:00","end":"07:50"}],"timezone":"Asia/Shanghai"}},
	 {"kind":"discount","enabled":false,"priority":99,"modelIds":["paid"],"discount":{"discountedCredits":"x0.1"}},
	 {"kind":"discount","enabled":true,"modelIds":["placeholder"],"discount":{"discountedCredits":"0x"}}
	]}`), &data); err != nil {
		t.Fatal(err)
	}
	p := Info{Rate: Number(1), Promotions: BuddyPromotions(data)["paid"]}
	for _, tc := range []struct {
		at   string
		want float64
	}{
		{"2026-11-05T23:30:00+08:00", 0}, {"2026-11-05T07:49:00+08:00", 0},
		{"2026-11-05T07:50:00+08:00", 1}, {"2026-11-05T16:00:00+08:00", 1},
		{"2026-11-07T23:30:00+08:00", .5},
	} {
		now, _ := time.Parse(time.RFC3339, tc.at)
		got := p.Effective(now)
		if got == nil || *got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.at, got, tc.want)
		}
	}
	if p.ConfirmedFree() {
		t.Fatal("temporary promotion authorized unattended spending")
	}
	if len(BuddyPromotions(data)["placeholder"]) != 0 {
		t.Fatal("unbounded zero placeholder became free")
	}
	clone := p.Clone()
	*clone.Rate = 9
	clone.Promotions[0].Daily[0].Start = "00:00"
	if *p.Rate != 1 || p.Promotions[0].Daily[0].Start != "23:00" {
		t.Fatal("clone changed published price")
	}
}

func TestQoderScheduledRateUsesCurrentWindow(t *testing.T) {
	var row map[string]any
	_ = json.Unmarshal([]byte(`{"price_factor":0.2,"promotion":{"active":true,"before_promotion_price_factor":0.5,"discount_factor":0.4,"window_start":"22:00","window_end":"08:00"}}`), &row)
	p := Parse(row)
	night, _ := time.Parse(time.RFC3339, "2026-11-05T23:00:00+08:00")
	day, _ := time.Parse(time.RFC3339, "2026-11-05T12:00:00+08:00")
	if *p.Effective(night) != .2 || *p.Effective(day) != .5 {
		t.Fatalf("stale Qoder price used all day: %+v", p)
	}
}
