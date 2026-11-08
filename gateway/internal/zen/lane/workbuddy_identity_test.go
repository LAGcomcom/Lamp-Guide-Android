package lane

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The three refusals this source can meet are unrelated verdicts that all
// arrive as a 4xx with similar-looking copy. Filing them together was the
// defect: the caller could not tell "rewrite your prompt" from "this
// account is burned" from "the request envelope is wrong".
func TestWB11140IsAnAccountVerdict(t *testing.T) {
	ue := classifyWorkBuddyFailure(403, `{"code":11140,"msg":"request illegal","displayMsg":{"en":"Your account has been restricted due to unusual activity"}}`, 0)
	if ue.Code != wbCodeRestricted {
		t.Fatalf("code = %q, want %q", ue.Code, wbCodeRestricted)
	}
	if ue.Code == CodeCredential {
		t.Fatal("an account restriction must not be filed as a credential fault: that sends the caller down a refresh-and-retry path that cannot help, and every replay is another strike")
	}
	if ue.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %d, want the first rung of the quarantine ladder", ue.RetryAfter)
	}
}

func TestWB11128IsARequestVerdict(t *testing.T) {
	ue := classifyWorkBuddyFailure(403, `{"code":11128,"msg":"Illegal API invocation from an unapproved channel"}`, 0)
	if ue.Code != wbCodeSanitize {
		t.Fatalf("code = %q, want the sanitize sentinel: a text verdict is retried once", ue.Code)
	}
}

// "request illegal" with no numeric code is the intl conversation-header
// fault. It reads like the 11128 family and is not: no prompt rewrite reaches
// it, so it must never enter the retry-once path.
func TestWBRequestIllegalIsAnEnvelopeFault(t *testing.T) {
	ue := classifyWorkBuddyFailure(400, `{"code":0,"msg":"request illegal"}`, 0)
	if ue.Code != CodeClient {
		t.Fatalf("code = %q, want CodeClient", ue.Code)
	}
	if !strings.Contains(ue.Message, "会话头") {
		t.Fatalf("the message must name the envelope, got %q", ue.Message)
	}
}

// The conversation id has to be stable across turns or the upstream prompt
// cache misses every turn; and it has to be 32 bare hex, the official shape.
func TestWBConversationIDIsStableAndBareHex(t *testing.T) {
	a := wbConversationID("session-seed")
	b := wbConversationID("session-seed")
	if a != b {
		t.Fatal("the same conversation must keep one upstream id, or the prompt cache never hits")
	}
	if len(a) != 32 {
		t.Fatalf("id = %q, want 32 bare hex: dashes and prefixes are a gateway signature", a)
	}
	if c := wbConversationID("other-seed"); c == a {
		t.Fatal("two conversations must not share an upstream id")
	}
	first, second := wbConversationID(""), wbConversationID("")
	if len(first) != 32 || first == second {
		t.Fatal("an empty seed (probe, daily claim) must mint a fresh id per turn")
	}
}

// The upstream reads "@ + long sentence" as a file reference and fails the
// whole request when it cannot resolve the path, under the same 11128 badge
// as a prompt match. Only a backslash escape clears it.
func TestWBEscapesMentionsWithoutBreakingPaths(t *testing.T) {
	in := "Tokens prefixed with @ are paths the user explicitly referenced."
	got := wbEscapeMentions(in)
	if !strings.Contains(got, `\@`) {
		t.Fatalf("the @ was not escaped: %q", got)
	}
	// A real path must survive byte-identical: inserting a character there
	// makes the model hand a broken path to a tool call.
	path := "D:\\10_COD\\DeepSeek Harness\\resources\\app.asar\\dsh\\"
	if wbEscapeMentions(path) != path {
		t.Fatal("a filesystem path was rewritten")
	}
	if s := wbEscapeMentions("mail me at a@b.com"); !strings.Contains(s, "a@b.com") {
		t.Fatalf("a mid-word @ is not a mention and must stay: %q", s)
	}
}

// The identity template is enforced on system/developer/assistant only: the
// same sentence passes from a user message and 400s from assistant history.
// Rewriting user content would desync the model's view from what is on disk.
func TestWBSanitizeIsRoleScoped(t *testing.T) {
	identity := "You are an AI agent powered by DeepSeek Harness."
	body := map[string]any{"messages": []map[string]any{
		{"role": "system", "content": identity},
		{"role": "assistant", "content": identity},
		{"role": "user", "content": identity},
	}}
	got := wbSanitize(body)
	msgs, _ := got["messages"].([]map[string]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3 shapes preserved", len(msgs))
	}
	for _, i := range []int{0, 1} {
		if s, _ := msgs[i]["content"].(string); strings.Contains(s, "powered by DeepSeek Harness") {
			t.Errorf("role %v still carries the template: %q", msgs[i]["role"], s)
		}
	}
	if s, _ := msgs[2]["content"].(string); !strings.Contains(s, "powered by DeepSeek Harness") {
		t.Error("user content was rewritten: the model would see something the user cannot")
	}
	// The caller's body must stay intact for the recovery paths.
	if orig, _ := body["messages"].([]map[string]any); len(orig) != 3 || orig[0]["content"] != identity {
		t.Fatal("wbSanitize mutated the caller body")
	}
}

// A bare 11128 in user data is a refusal condition wherever it appears, but a
// longer digit run is ordinary data: the old bare substring replace silently
// corrupted timestamps the user asked the model to read.
func TestWBBareCodeRewriteKeepsDigitRuns(t *testing.T) {
	got := wbRewriteText("ts=1729111280 order 900111285 code 11128")
	if strings.Contains(got, "ts=172911-1280") || strings.Contains(got, "90011-1285") {
		t.Fatalf("ordinary digits were rewritten: %q", got)
	}
	if !strings.Contains(got, "11-128") {
		t.Fatalf("the bare code was not neutralized: %q", got)
	}
}

// A restricted account is parked instead of replayed: the restriction is a
// verdict about this credential's traffic, and repeats are punished harder
// than first offences.
func TestWBRestrictedAccountIsParkedNotReplayed(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(403)
		fmt.Fprint(w, `{"code":11140,"msg":"request illegal","displayMsg":{"en":"Your account has been restricted due to unusual activity"}}`)
	}));
	defer srv.Close()
	old := WorkBuddyBase
	WorkBuddyBase = srv.URL
	t.Cleanup(func() { WorkBuddyBase = old })

	l := NewLane()
	l.SetWorkBuddyAccount(wbAcct())
	_, err := l.wbPost(context.Background(), map[string]any{}, "", func([]byte) error { return nil })
	if err == nil {
		t.Fatal("the refusal must surface")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("upstream was hit %d times, want 1: a restricted account must not be replayed", n)
	}
	// The next turn must answer from the local ledger, not from a round trip.
	_, err2 := l.wbPost(context.Background(), map[string]any{}, "", func([]byte) error { return nil })
	if ue := asUpstream(err2); ue == nil || ue.Code != wbCodeRestricted {
		t.Fatalf("second call = %v, want a local restriction verdict", err2)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("upstream was hit %d times, want the second turn answered locally", n)
	}
}

// Every WorkBuddy POST opens with a system message: the gate refuses a
// conversation that starts with the user, which is exactly what a plain
// OpenAI-shaped body looks like. Hand-built bodies (probe, daily claim) go
// through the same door, so the rule lives at the POST, not at the caller.
func TestWBEnsuresSystemFirst(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, wbChatSSE(`{"choices":[{"delta":{},"finish_reason":"stop"}]}`))
	}));
	defer srv.Close()
	old := WorkBuddyBase
	WorkBuddyBase = srv.URL
	t.Cleanup(func() { WorkBuddyBase = old })

	body := map[string]any{"messages": []map[string]any{{"role": RoleUser, "content": "ping"}}}
	_, err := PostWorkBuddyStreamed(context.Background(), body, wbAcct(), "", func([]byte) error { return nil })
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	var sent struct {
		Messages []map[string]any `json:"messages"`
	}
	if jerr := json.Unmarshal([]byte(gotBody), &sent); jerr != nil {
		t.Fatalf("sent body is not JSON: %v", jerr)
	}
	if len(sent.Messages) != 2 || sent.Messages[0]["role"] != RoleSystem {
		t.Fatalf("first message role = %v, want the system preamble", sent.Messages[0]["role"])
	}
	// A caller that already leads with a system prompt keeps its own text.
	body2 := map[string]any{"messages": []map[string]any{{"role": RoleSystem, "content": "mine"}, {"role": RoleUser, "content": "hi"}}}
	wbEnsureSystemFirst(body2)
	if msgs, _ := body2["messages"].([]map[string]any); len(msgs) != 2 || msgs[0]["content"] != "mine" {
		t.Fatal("an existing leading system prompt must be preserved, not shadowed")
	}
}

// The quarantine ladder is the whole point: a restriction cannot be probed
// away, so the only honest move is to stop and wait longer each time.
func TestWBRestrictionLadderEscalates(t *testing.T) {
	prev := time.Duration(0)
	for i := 1; i <= 4; i++ {
		w := wbRestrictionWait(i)
		if w < prev {
			t.Fatalf("strike %d shortened the window from %v to %v", i, prev, w)
		}
		prev = w
	}
	if w := wbRestrictionWait(99); w != prev {
		t.Fatalf("beyond the table the ladder must pin at the cap, got %v", w)
	}
}
