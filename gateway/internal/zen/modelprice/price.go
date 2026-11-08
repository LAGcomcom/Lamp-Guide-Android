// Package modelprice preserves upstream credit rates and evaluates scheduled
// promotions at read time. Token prices are deliberately not credit multipliers.
package modelprice

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

type Window struct {
	Start string `json:"start"`
	End   string `json:"end"`
}
type Promotion struct {
	Rate       float64  `json:"rate"`
	Priority   float64  `json:"priority,omitempty"`
	ValidFrom  string   `json:"validFrom,omitempty"`
	ValidUntil string   `json:"validUntil,omitempty"`
	Timezone   string   `json:"timezone,omitempty"`
	Daily      []Window `json:"daily,omitempty"`
}
type Info struct {
	Rate       *float64    `json:"rate,omitempty"`
	Promotions []Promotion `json:"promotions,omitempty"`
	Free       bool        `json:"free,omitempty"`
}

func Number(v float64) *float64 { return &v }
func (p Info) Clone() Info {
	if p.Rate != nil {
		p.Rate = Number(*p.Rate)
	}
	p.Promotions = append([]Promotion(nil), p.Promotions...)
	for i := range p.Promotions {
		p.Promotions[i].Daily = append([]Window(nil), p.Promotions[i].Daily...)
	}
	return p
}

var rateRE = regexp.MustCompile(`(?i)^(?:[x×]([0-9]+(?:\.[0-9]+)?)|([0-9]+(?:\.[0-9]+)?)[x×])(?:\s+credits)?$`)

func ParseRate(v any) *float64 {
	var n float64
	switch t := v.(type) {
	case float64:
		n = t
	case string:
		m := rateRE.FindStringSubmatch(strings.TrimSpace(t))
		if m == nil {
			return nil
		}
		s := m[1]
		if s == "" {
			s = m[2]
		}
		n, _ = strconv.ParseFloat(s, 64)
	default:
		return nil
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return nil
	}
	return Number(n)
}

func minutes(v string) (int, bool) {
	parts := strings.Split(v, ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, e := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	return h*60 + m, e == nil && e2 == nil && h >= 0 && h < 24 && m >= 0 && m < 60
}
func (p Promotion) Active(now time.Time) bool {
	for _, b := range []struct {
		value string
		start bool
	}{{p.ValidFrom, true}, {p.ValidUntil, false}} {
		if b.value == "" {
			continue
		}
		t, e := time.Parse(time.RFC3339, b.value)
		if e != nil {
			return false
		}
		if b.start && now.Before(t) || !b.start && !now.Before(t) {
			return false
		}
	}
	if len(p.Daily) == 0 {
		return true
	}
	zone := p.Timezone
	if zone == "" {
		zone = "Asia/Shanghai"
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return false
	}
	t := now.In(loc)
	m := t.Hour()*60 + t.Minute()
	for _, w := range p.Daily {
		a, ok := minutes(w.Start)
		b, ok2 := minutes(w.End)
		if !ok || !ok2 {
			continue
		}
		if a <= b && m >= a && m < b || a > b && (m >= a || m < b) {
			return true
		}
	}
	return false
}
func (p Info) Effective(now time.Time) *float64 {
	var best *Promotion
	for i := range p.Promotions {
		promo := &p.Promotions[i]
		if promo.Active(now) && (best == nil || promo.Priority >= best.Priority) {
			best = promo
		}
	}
	if best != nil {
		return Number(best.Rate)
	}
	if p.Rate != nil {
		return Number(*p.Rate)
	}
	if p.Free {
		return Number(0)
	}
	return nil
}

// ConfirmedFree requires an undiscounted zero: a promotion can end while an
// unattended request is in flight, so it must never authorize background work.
func (p Info) ConfirmedFree() bool { return p.Rate != nil && *p.Rate == 0 || p.Rate == nil && p.Free }

func stringValue(v any) string    { s, _ := v.(string); return s }
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }

// Parse reads explicit credit fields only; "pricing.input" etc. have different units.
func Parse(row map[string]any) Info {
	p := Info{}
	for _, key := range []string{"creditsRate", "credits", "price_factor", "priceFactor"} {
		if n := ParseRate(row[key]); n != nil {
			p.Rate = n
			break
		}
	}
	p.Free = row["is_free"] == true || row["isFree"] == true
	if original := ParseRate(row["original_price_factor"]); original != nil && p.Rate != nil && *original != *p.Rate {
		p.Promotions = []Promotion{{Rate: *p.Rate}}
		p.Rate = original
	}
	if d := ParseRate(row["discountedCreditsRate"]); d != nil {
		p.Promotions = append(p.Promotions, Promotion{Rate: *d})
	}
	if promo := object(row["promotion"]); promo != nil {
		base := ParseRate(promo["before_promotion_price_factor"])
		factor, ok := promo["discount_factor"].(float64)
		if base != nil && ok && factor >= 0 {
			p.Rate = base
			p.Promotions = nil
			a, b := stringValue(promo["window_start"]), stringValue(promo["window_end"])
			if a != "" && b != "" {
				p.Promotions = []Promotion{{Rate: *base * factor, Timezone: "Asia/Shanghai", Daily: []Window{{a, b}}}}
			} else if promo["active"] == true {
				p.Promotions = []Promotion{{Rate: *base * factor}}
			}
		}
	}
	return p
}

// BuddyPromotions follows the plugin's schedule/priority rules, including
// cross-midnight windows and zero-rate promotions with a real time window.
func BuddyPromotions(data map[string]any) map[string][]Promotion {
	out := map[string][]Promotion{}
	rows, _ := data["modelPromotions"].([]any)
	for _, raw := range rows {
		r := object(raw)
		if r == nil || r["enabled"] == false || r["kind"] != "discount" {
			continue
		}
		d := object(r["discount"])
		rate := ParseRate(d["discountedCredits"])
		if rate == nil {
			continue
		}
		s := object(r["schedule"])
		p := Promotion{Rate: *rate, ValidFrom: stringValue(s["validFrom"]), ValidUntil: stringValue(s["validUntil"]), Timezone: stringValue(s["timezone"])}
		p.Priority, _ = r["priority"].(float64)
		if daily, ok := s["daily"].([]any); ok {
			for _, v := range daily {
				w := object(v)
				p.Daily = append(p.Daily, Window{stringValue(w["start"]), stringValue(w["end"])})
			}
		}
		if *rate == 0 && p.ValidFrom == "" && p.ValidUntil == "" && len(p.Daily) == 0 {
			continue
		}
		ids, _ := r["modelIds"].([]any)
		for _, id := range ids {
			if key := stringValue(id); key != "" {
				out[key] = append(out[key], p)
			}
		}
	}
	return out
}
