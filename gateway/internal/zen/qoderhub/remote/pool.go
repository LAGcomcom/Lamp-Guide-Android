package remote

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

type credentialSlot struct {
	credential Credential
	cool       map[string]time.Time
	inFlight   int
	failures   int
}

type chatStatusError struct {
	status  int
	message string
}

func (e *chatStatusError) Error() string {
	return fmt.Sprintf("remote chat status %d: %s", e.status, e.message)
}

func credentialKey(cred Credential) string {
	if cred.UserID != "" {
		return cred.UserID
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(cred.CosyKey+cred.AccessToken)))
}

func (c *Client) credentials() ([]Credential, error) {
	if c.cfg.Credentials != nil {
		if accts := c.cfg.Credentials(); accts != nil {
			return accts, nil
		}
	}
	cred, err := LoadCredential(c.cfg.AuthFile)
	if err != nil {
		return nil, err
	}
	return []Credential{cred}, nil
}

func (c *Client) loadCredential() (Credential, error) {
	accts, err := c.credentials()
	if err != nil {
		return Credential{}, err
	}
	if len(accts) == 0 {
		return Credential{}, errors.New("Qoder 账号已退出")
	}
	c.poolMu.Lock()
	active := c.activeCredential
	c.poolMu.Unlock()
	for _, cred := range accts {
		if credentialKey(cred) == active {
			return cred, nil
		}
	}
	return accts[0], nil
}

// Sequential exhaustion keeps the active identity for prompt caching. A quota
// failure switches accounts for this model; content already sent is never replayed.
func (c *Client) chatPool(ctx context.Context, req ChatRequest, emit func(StreamEvent)) (*ChatResult, error) {
	accts, err := c.credentials()
	if err != nil {
		return nil, err
	}
	if len(accts) == 0 {
		return nil, errors.New("Qoder 账号已退出")
	}
	c.poolMu.Lock()
	if c.pool == nil {
		c.pool = map[string]*credentialSlot{}
	}
	keys := make([]string, 0, len(accts))
	seen := map[string]bool{}
	for _, cred := range accts {
		key := credentialKey(cred)
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
		slot := c.pool[key]
		if slot == nil || slot.credential != cred {
			c.pool[key] = &credentialSlot{credential: cred, cool: map[string]time.Time{}}
		}
	}
	for key := range c.pool {
		if !seen[key] {
			delete(c.pool, key)
		}
	}
	start := 0
	for i, key := range keys {
		if key == c.activeCredential {
			start = i
			break
		}
	}
	c.poolMu.Unlock()
	var last error
	attempts := 0
	for index := 0; index < len(keys) && attempts < 4; index++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		key := keys[(start+index)%len(keys)]
		c.poolMu.Lock()
		slot := c.pool[key]
		if slot == nil || slot.inFlight >= 2 || time.Now().Before(slot.cool["*"]) || time.Now().Before(slot.cool[req.Model]) {
			c.poolMu.Unlock()
			continue
		}
		slot.inFlight++
		attempts++
		cred := slot.credential
		c.activeCredential = key
		c.poolMu.Unlock()
		sawContent := false
		out, err := c.chatCredential(ctx, req, cred, func(ev StreamEvent) {
			if ev.Delta != "" || ev.ToolCall != nil {
				sawContent = true
			}
			if emit != nil {
				emit(ev)
			}
		})
		c.poolMu.Lock()
		slot.inFlight--
		retry := false
		if err == nil {
			slot.failures = 0
		} else {
			var statusErr *chatStatusError
			if errors.As(err, &statusErr) {
				switch {
				case statusErr.status == 429:
					slot.cool[req.Model] = time.Now().Add(10 * time.Minute)
					retry = true
				case statusErr.status == 401 || statusErr.status == 403 || statusErr.status == 402:
					slot.cool["*"] = time.Now().Add(30 * time.Minute)
					retry = true
				case statusErr.status >= 500:
					retry = true
				}
			} else {
				retry = ctx.Err() == nil
			}
			if retry {
				slot.failures++
				if slot.failures >= 3 {
					slot.cool["*"] = time.Now().Add(30 * time.Minute)
				}
			}
		}
		c.poolMu.Unlock()
		if err == nil || sawContent || !retry || ctx.Err() != nil {
			return out, err
		}
		last = err
	}
	if last != nil {
		return nil, last
	}
	return nil, errors.New("Qoder 账号都在冷却或已达到并发上限")
}
