package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"zenui/internal/zen/channels"
	"zenui/internal/zen/checkin"
	"zenui/internal/zen/chprov"
	"zenui/internal/zen/lane"
	"zenui/internal/zen/relay"
	"zenui/internal/zen/store"
)

type nativeCatalogEntry struct {
	Models []chprov.NativeModel
	Error  string
}

func (s *Server) nativeChannel(id string) bool { return chprov.IsNativeChannel(id) }

func nativeCatalogKey(id string, a chprov.Account) string {
	// Only catalog authorization matters. Renewing a separate gateway key must
	// not discard the directory, while replacing the session must invalidate it.
	b, _ := json.Marshal([]string{id, a.Key(), a.AccessToken, a.Field("organization_code"), a.Field("account_id")})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func (s *Server) nativeContext(ctx context.Context) context.Context {
	if s.checkinClient != nil {
		return chprov.WithCheckinClient(ctx, s.checkinClient)
	}
	return ctx
}

func (s *Server) nativeCatalogLoop(ctx context.Context) {
	s.refreshNativeCatalogs(ctx)
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.refreshNativeCatalogs(ctx)
		}
	}
}

func (s *Server) queueNativeCatalogRefresh() {
	// A server without a lifecycle context is an isolated API/test host.
	if s.checkinCtx != nil {
		go s.refreshNativeCatalogs(s.checkinCtx)
	}
}

func (s *Server) refreshNativeCatalogs(ctx context.Context) CatalogRefreshReport {
	s.nativeRefreshMu.Lock()
	defer s.nativeRefreshMu.Unlock()
	report := CatalogRefreshReport{Updated: []string{}, Failed: []string{}}
	next := map[string]nativeCatalogEntry{}
	for _, id := range chprov.NativeChannelIDs {
		c, _ := channels.ByID(id)
		accts := s.chanAccounts(id).Usable()
		if len(accts) == 0 {
			continue
		}
		failed := false
		for _, a := range accts {
			if ctx.Err() != nil {
				return report
			}
			fresh, err := s.prepareNative(ctx, id, a, false)
			if err != nil {
				next[nativeCatalogKey(id, a)] = nativeCatalogEntry{Error: "账号需重新登录或续期失败"}
				failed = true
				continue
			}
			models, err := chprov.NativeCatalog(s.nativeContext(ctx), id, fresh)
			entry := nativeCatalogEntry{Models: models}
			if err != nil {
				entry.Models, entry.Error, failed = nil, err.Error(), true
			}
			next[nativeCatalogKey(id, fresh)] = entry
		}
		if failed {
			report.Failed = append(report.Failed, c.Name)
		} else {
			report.Updated = append(report.Updated, c.Name)
		}
	}
	if ctx.Err() != nil {
		return report
	}
	s.nativeCatalogMu.Lock()
	s.nativeCatalog = next
	s.nativeCatalogMu.Unlock()
	if s.modelsSync != nil {
		s.modelsSync()
	}
	return report
}

func (s *Server) nativeModels() map[string][]chprov.NativeModel {
	out := map[string][]chprov.NativeModel{}
	for _, id := range chprov.NativeChannelIDs {
		seen := map[string]bool{}
		for _, a := range s.chanAccounts(id).Usable() {
			s.nativeCatalogMu.RLock()
			entry := s.nativeCatalog[nativeCatalogKey(id, a)]
			s.nativeCatalogMu.RUnlock()
			for _, m := range entry.Models {
				if !seen[m.ID] {
					seen[m.ID] = true
					out[id] = append(out[id], m)
				}
			}
		}
	}
	return out
}

func (s *Server) prepareNative(ctx context.Context, id string, a chprov.Account, inference bool) (chprov.Account, error) {
	s.nativeAuthMu.Lock()
	defer s.nativeAuthMu.Unlock()
	// Resolve the current snapshot after acquiring the refresh lock.
	found := false
	for _, current := range s.chanAccounts(id).Usable() {
		if current.Key() == a.Key() {
			a, found = current, true
			break
		}
	}
	if !found {
		return a, fmt.Errorf("账号已退出")
	}
	var fresh chprov.Account
	var err error
	if inference {
		fresh, err = chprov.PrepareNativeAccount(s.nativeContext(ctx), id, a)
	} else {
		var failure *checkin.Outcome
		fresh, failure = chprov.CheckinAccount(s.nativeContext(ctx), id, a)
		if failure != nil {
			err = fmt.Errorf("%s", failure.Message)
		}
	}
	if err != nil {
		return a, err
	}
	if reflect.DeepEqual(a, fresh) {
		return fresh, nil
	}
	extra := make(map[string]string, len(fresh.Extra)+1)
	for k, v := range fresh.Extra {
		extra[k] = v
	}
	extra["lamp_checkin_key"] = checkinKey(id, a)
	fresh.Extra = extra
	updated := false
	s.Store.Mutate(func(cfg *store.Config) {
		for i, current := range cfg.ChannelAccounts[id] {
			if reflect.DeepEqual(current, a) {
				cfg.ChannelAccounts[id][i] = fresh
				updated = true
				break
			}
		}
	})
	if !updated {
		return a, fmt.Errorf("账号在续期期间发生变化")
	}
	if err = s.Store.Save(); err != nil {
		return a, fmt.Errorf("凭据保存失败，未发送模型请求")
	}
	return fresh, nil
}

func (s *Server) nativeTurn(ctx context.Context, req lane.Request, id, model string, emit func(lane.Chunk)) (lane.Outcome, *lane.UpstreamError) {
	last := &lane.UpstreamError{Code: lane.CodeCredential, Message: "渠道没有已登录且可用的账号"}
	if !aggregateChannelEnabled(s.Store.Config(), id) {
		return lane.Outcome{}, &lane.UpstreamError{Code: lane.CodeClient, Message: "渠道已停用聚合"}
	}
	quotas := s.chanQuotas(id)
	accts := s.chanAccounts(id).Usable()
	sort.SliceStable(accts, func(i, j int) bool {
		a, b := quotas[accts[i].Key()], quotas[accts[j].Key()]
		if a.Known != b.Known {
			return a.Known
		}
		return a.Known && a.ExpiryMS < b.ExpiryMS
	})
	for _, a := range accts {
		if ctx.Err() != nil {
			return lane.Outcome{}, &lane.UpstreamError{Code: lane.CodeAborted, Message: "已取消"}
		}
		q := quotas[a.Key()]
		if !q.Never && !(q.Known && q.ExpiryMS > time.Now().UnixMilli()) {
			continue
		}
		s.nativeCatalogMu.RLock()
		entry := s.nativeCatalog[nativeCatalogKey(id, a)]
		s.nativeCatalogMu.RUnlock()
		var selected *chprov.NativeModel
		for i := range entry.Models {
			if entry.Models[i].ID == model {
				selected = &entry.Models[i]
				break
			}
		}
		if selected == nil {
			continue
		}
		fresh, err := s.prepareNative(ctx, id, a, true)
		if err != nil {
			last = &lane.UpstreamError{Code: lane.CodeCredential, Message: err.Error()}
			continue
		}
		// Recheck after the network-bound credential step: logout and disabling a
		// model/channel while it was running must prevent the outbound request.
		present := false
		for _, current := range s.chanAccounts(id).Usable() {
			if reflect.DeepEqual(current, fresh) {
				present = true
				break
			}
		}
		cfg := s.Store.Config()
		blocked := !aggregateChannelEnabled(cfg, id)
		for _, key := range cfg.AggregateExclude {
			if key == id+"|"+model {
				blocked = true
			}
		}
		if !present || blocked {
			continue
		}
		channel, _ := channels.ByID(id)
		p := &store.Provider{ID: id, Name: channel.Name, Protocol: store.ProtocolOpenAI, BaseURL: channel.BaseURL, APIKey: fresh.AccessToken, Enabled: true}
		if strings.HasPrefix(id, "magicrew") {
			p.APIKey = fresh.Field("api_key")
		}
		client := *lane.Client()
		if s.checkinClient != nil {
			client = *s.checkinClient
		}
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		hooks := &relay.UpstreamOptions{Headers: chprov.NativeChatHeaders(id, fresh, model), Client: &client}
		hooks.BuildBody = func(body map[string]any) {
			if selected.MaxOutput > 0 {
				if n, ok := body["max_tokens"].(int); ok && n > selected.MaxOutput {
					body["max_tokens"] = selected.MaxOutput
				}
			}
			if strings.HasPrefix(id, "autoclaw") {
				const prefix = "You are AutoClaw. Answer the user directly and concisely"
				messages, _ := body["messages"].([]map[string]any)
				// ToChatMessages returns []map[string]any; preserve all caller instructions.
				body["messages"] = append([]map[string]any{{"role": "system", "content": prefix}}, messages...)
			}
			if id == "astudio" {
				if uid := fresh.Field("account_id"); uid != "" {
					body["user"] = uid
				}
			}
		}
		if id == "astudio" {
			hooks.RewriteFrame = astudioFrameRewriter()
		}
		if req.OnAttempt != nil {
			req.OnAttempt(id + "/" + model)
		}
		saw := false
		usage, finish, upstreamErr := relay.Complete(ctx, relay.Turn{Provider: p, Model: model, Messages: req.Messages, Tools: req.Tools, MaxTokens: req.MaxTokens, Agent: req.Agent, Upstream: hooks}, func(chunk lane.Chunk) {
			if chunk.Kind == lane.ChunkTextDelta || chunk.Kind == lane.ChunkReasonDelta || chunk.Kind == lane.ChunkToolCallDelta || chunk.Kind == lane.ChunkBlockStart {
				saw = true
			}
			emit(chunk)
		})
		if upstreamErr == nil && finish == "" {
			upstreamErr = &lane.UpstreamError{Code: lane.CodeTransport, Message: "上游未返回有效的结束事件"}
		}
		s.recordRelay(ctx, p, model, req.Agent, finish != "", usage, 0, finish, upstreamErr)
		out := lane.Outcome{Usage: usage, Finish: finish, ServedModel: id + "/" + model}
		if upstreamErr == nil || saw {
			return out, upstreamErr
		}
		last = upstreamErr
	}
	return lane.Outcome{}, last
}

// AStudio can send accumulated content/reasoning/tool arguments. Convert each
// choice and tool slot independently so callers see each byte exactly once.
func astudioFrameRewriter() func([]byte) []byte {
	state := map[string]string{}
	return func(payload []byte) []byte {
		var root map[string]any
		if json.Unmarshal(payload, &root) != nil {
			return payload
		}
		rows, _ := root["choices"].([]any)
		for index, row := range rows {
			r, _ := row.(map[string]any)
			d, _ := r["delta"].(map[string]any)
			key := fmt.Sprint(root["id"]) + ":" + fmt.Sprint(r["index"]) + ":" + strconv.Itoa(index)
			rewrite := func(m map[string]any, field, slot string) {
				incoming, ok := m[field].(string)
				if !ok {
					return
				}
				before := state[slot]
				after := incoming
				if strings.HasPrefix(incoming, before) {
					after = strings.TrimPrefix(incoming, before)
				} else if strings.HasPrefix(before, incoming) {
					after = ""
				}
				m[field], state[slot] = after, before+after
			}
			rewrite(d, "content", key+":text")
			rewrite(d, "reasoning_content", key+":reason")
			calls, _ := d["tool_calls"].([]any)
			for _, call := range calls {
				c, _ := call.(map[string]any)
				f, _ := c["function"].(map[string]any)
				rewrite(f, "arguments", key+":tool:"+fmt.Sprint(c["index"]))
			}
		}
		b, err := json.Marshal(root)
		if err != nil {
			return payload
		}
		return b
	}
}
