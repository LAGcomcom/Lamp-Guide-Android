package lane

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"zenui/internal/zen/chprov"
)

// Each slot retains the existing single-account refresh and device identity
// logic. Runtime cooldowns belong to this account/model, never the whole lane.
type wbPoolSlot struct {
	key      string
	seed     WBAccount
	client   *Lane
	cool     map[string]time.Time
	inFlight int
	failures int
}

func wbPoolKey(a WBAccount) string {
	if id := strings.TrimSpace(a.UID); id != "" && id != "anonymous" {
		return id
	}
	return fmt.Sprintf("token:%x", sha256.Sum256([]byte(a.AccessToken)))
}

func WBAccountsFromChannel(accts chprov.Accounts, fallback WBAccount) []WBAccount {
	var out []WBAccount
	seen := map[string]bool{}
	for _, a := range accts.Usable() {
		if strings.TrimSpace(a.AccessToken) == "" {
			continue
		}
		x := WBAccountFromChannel(chprov.Accounts{a}, WBAccount{})
		if wbPoolKey(x) == wbPoolKey(fallback) {
			x.LastActivityDate = fallback.LastActivityDate
		}
		if !seen[wbPoolKey(x)] {
			out = append(out, x)
			seen[wbPoolKey(x)] = true
		}
	}
	if fallback.AccessToken != "" && !seen[wbPoolKey(fallback)] {
		out = append(out, fallback)
	}
	return out
}

// SetWorkBuddyChannels is used at boot, login and settings reload. The global
// enable switch gates all stored accounts without deleting their credentials.
func (l *Lane) SetWorkBuddyChannels(accts chprov.Accounts, fallback WBAccount) {
	pool := WBAccountsFromChannel(accts, fallback)
	for i := range pool {
		pool[i].Enabled = fallback.Enabled
	}
	l.SetWorkBuddyAccounts(pool)
}

func (l *Lane) SetWorkBuddyAccounts(accts []WBAccount) {
	if len(accts) < 2 {
		var a WBAccount
		if len(accts) == 1 {
			a = accts[0]
		}
		l.SetWorkBuddyAccount(a)
		return
	}
	l.wbMu.Lock()
	prev := make(map[string]*wbPoolSlot, len(l.wbPool))
	for _, slot := range l.wbPool {
		prev[slot.key] = slot
	}
	next := make([]*wbPoolSlot, 0, len(accts))
	activeExists := false
	for _, acct := range accts {
		key := wbPoolKey(acct)
		slot := prev[key]
		if slot == nil || slot.seed.AccessToken != acct.AccessToken || slot.seed.Enabled != acct.Enabled {
			child := &Lane{wbAcct: acct, wbPersist: l.wbPoolPersist(key)}
			slot = &wbPoolSlot{key: key, seed: acct, client: child, cool: map[string]time.Time{}}
		}
		next = append(next, slot)
		activeExists = activeExists || key == l.wbActive
	}
	if !activeExists {
		l.wbActive = next[0].key
	}
	l.wbPool = next
	l.wbAcct = accts[0]
	l.wbMu.Unlock()
}

func (l *Lane) wbPoolPersist(key string) func(WBAccount) {
	return func(a WBAccount) {
		l.wbMu.Lock()
		found := false
		for _, slot := range l.wbPool {
			if slot.key == key {
				slot.seed = a
				found = true
				break
			}
		}
		if key == l.wbActive {
			l.wbAcct = a
		}
		fn := l.wbPersist
		l.wbMu.Unlock()
		// A refresh finishing after logout must never resurrect the account.
		if found && fn != nil {
			fn(a)
		}
	}
}

func (l *Lane) wbPost(ctx context.Context, body map[string]any, conversationID string, onData func([]byte) error) (*Usage, error) {
	l.wbMu.Lock()
	pooled := len(l.wbPool) > 0
	l.wbMu.Unlock()
	if !pooled {
		return l.wbPostSingle(ctx, body, conversationID, onData)
	}
	model, _ := body["model"].(string)
	tried := map[string]bool{}
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		if ctx.Err() != nil {
			return nil, &UpstreamError{Code: CodeAborted, Message: "cancelled"}
		}
		l.wbMu.Lock()
		start := 0
		for i, slot := range l.wbPool {
			if slot.key == l.wbActive {
				start = i
				break
			}
		}
		var picked *wbPoolSlot
		for i := 0; i < len(l.wbPool); i++ {
			slot := l.wbPool[(start+i)%len(l.wbPool)]
			if tried[slot.key] || !slot.seed.Enabled || slot.inFlight >= 2 || time.Now().Before(slot.cool[model]) || time.Now().Before(slot.cool["*"]) {
				continue
			}
			picked = slot
			slot.inFlight++
			l.wbActive = slot.key
			break
		}
		l.wbMu.Unlock()
		if picked == nil {
			break
		}
		tried[picked.key] = true
		sawData := false
		usage, err := picked.client.wbPostSingle(ctx, body, conversationID, func(payload []byte) error {
			// Once any stream data reached the caller, replay is forbidden.
			sawData = true
			return onData(payload)
		})
		l.wbMu.Lock()
		picked.inFlight--
		ue := asUpstream(err)
		if err == nil {
			picked.failures = 0
		} else if ue != nil {
			switch ue.Code {
			case CodeQuota:
				wait := ue.RetryAfter
				if wait <= 0 {
					wait = 600
				}
				picked.cool[model] = time.Now().Add(time.Duration(wait) * time.Second)
			case CodeCredential:
				picked.cool["*"] = time.Now().Add(24 * time.Hour)
			case wbCodeRestricted:
				picked.cool["*"] = time.Now().Add(wbRestrictionWait(1))
			case CodeServer, CodeTransport:
				picked.failures++
				if picked.failures >= 3 {
					picked.cool["*"] = time.Now().Add(30 * time.Minute)
				}
			}
		}
		l.wbMu.Unlock()
		if err == nil || sawData || ctx.Err() != nil || ue == nil || (ue.Code != CodeQuota && ue.Code != CodeCredential && ue.Code != wbCodeRestricted && ue.Code != CodeServer && ue.Code != CodeTransport) {
			return usage, err
		}
		last = err
	}
	if last != nil {
		return nil, last
	}
	return nil, &UpstreamError{Code: CodeQuota, Message: "WorkBuddy 账号都在冷却或已达到并发上限", RetryAfter: 30}
}
