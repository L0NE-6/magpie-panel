package gateway

import (
	"context"
	"net"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/yetone/magpie/internal/provider"
)

// A relay serving Gemini on chat completions under a host of its own
// (Mamba_asr on X: "peta-gemini", gemini-4-argon) gives a call's thought
// signature in extra_content as Google's API does, and turns the next
// turn away without it: 400 "missing a thought_signature". magpie gave
// the signature back to Gemini's own host and to a local one only, so
// on the relay every second tool turn failed, from Claude Code, Codex
// and a Chat client that keeps no extra_content alike. A signature goes
// back to the upstream that gave it, whatever its host.
func TestRelayGeminiSignatureGoesBack(t *testing.T) {
	const sig = "CiQBcGV0YS1nZW1pbmktc2lnbmF0dXJlLWJ5dGVzLS0+Pz8/Pw=="
	tools := map[string]string{
		"/v1/chat/completions": `[{"type":"function","function":{"name":"bash","parameters":{"type":"object"}}}]`,
		"/v1/messages":         `[{"name":"bash","input_schema":{"type":"object"}}]`,
		"/v1/responses":        `[{"type":"function","name":"bash","parameters":{"type":"object"}}]`,
	}
	first := map[string]string{
		"/v1/chat/completions": `{"model":"peta-gemini/gemini-4-argon","stream":%s,"messages":[{"role":"user","content":"list files"}],"tools":%t}`,
		"/v1/messages":         `{"model":"peta-gemini/gemini-4-argon","max_tokens":1000,"stream":false,"messages":[{"role":"user","content":"list files"}],"tools":%t}`,
		"/v1/responses":        `{"model":"peta-gemini/gemini-4-argon","stream":false,"input":[{"role":"user","content":"list files"}],"tools":%t}`,
	}
	// the next turn as each client keeps the call: its id, and no
	// extra_content
	second := map[string]string{
		"/v1/chat/completions": `{"model":"peta-gemini/gemini-4-argon","stream":true,"messages":[
			{"role":"user","content":"list files"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"%id","type":"function","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}}]},
			{"role":"tool","tool_call_id":"%id","content":"a.txt"}],"tools":%t}`,
		"/v1/messages": `{"model":"peta-gemini/gemini-4-argon","max_tokens":1000,"stream":false,"messages":[
			{"role":"user","content":"list files"},
			{"role":"assistant","content":[{"type":"tool_use","id":"%id","name":"bash","input":{"command":"ls"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"%id","content":"a.txt"}]}],"tools":%t}`,
		"/v1/responses": `{"model":"peta-gemini/gemini-4-argon","stream":false,"input":[
			{"role":"user","content":"list files"},
			{"type":"function_call","call_id":"%id","name":"bash","arguments":"{\"command\":\"ls\"}"},
			{"type":"function_call_output","call_id":"%id","output":"a.txt"}],"tools":%t}`,
	}
	idIn := regexp.MustCompile(`"(?:id|call_id)":"(function-call-7[^"]*)"`)
	type run struct {
		path   string
		stream bool
	}
	for _, c := range []run{{"/v1/chat/completions", true}, {"/v1/chat/completions", false}, {"/v1/messages", true}, {"/v1/responses", true}} {
		t.Run(strings.TrimPrefix(c.path, "/v1/")+map[bool]string{true: "-streamed", false: "-whole"}[c.stream], func(t *testing.T) {
			up, bodies := geminiSigUpstream(t, sig, c.stream)
			fresh(t)
			p := provider.Provider{ID: "peta-gemini", Name: "peta-gemini", Key: "k", Models: []string{"gemini-4-argon"}, Chat: "http://api.peta.example.com/v1"}
			if err := provider.Save(p); err != nil {
				t.Fatal(err)
			}
			s := New()
			addr := strings.TrimPrefix(up.URL, "http://")
			s.client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			}}}
			fill := func(b, id string) string {
				b = strings.ReplaceAll(b, "%t", tools[c.path])
				b = strings.ReplaceAll(b, "%s", map[bool]string{true: "true", false: "false"}[c.stream])
				return strings.ReplaceAll(b, "%id", id)
			}
			out := sigPost(t, s, c.path, fill(first[c.path], ""))
			m := idIn.FindStringSubmatch(out)
			if m == nil {
				t.Fatalf("no call in the reply:\n%s", out)
			}
			sigPost(t, s, c.path, fill(second[c.path], m[1]))
			sent := (*bodies)[len(*bodies)-1]
			var call, result map[string]any
			for _, m := range sent["messages"].([]any) {
				m := m.(map[string]any)
				if cs, ok := m["tool_calls"].([]any); ok {
					call = cs[0].(map[string]any)
				}
				if m["role"] == "tool" {
					result = m
				}
			}
			if call == nil || result == nil {
				t.Fatalf("the relay was sent %v", sent["messages"])
			}
			extra, _ := call["extra_content"].(map[string]any)
			google, _ := extra["google"].(map[string]any)
			if google["thought_signature"] != sig || call["id"] != "function-call-7" || result["tool_call_id"] != "function-call-7" {
				t.Fatalf("the relay was sent the call %v and the result for %v", call, result["tool_call_id"])
			}
		})
	}
}

// A Chat client can keep the signature in extra_content with a plain call id.
func TestChatRelayWithExplicitSignature(t *testing.T) {
	const sig = "c2lnbmVkLWNhbGw="
	const body = `{"model":"fake/m1","messages":[{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"read","arguments":"{}"},"extra_content":{"google":{"thought_signature":"` + sig + `","other":1},"vendor_x":true}}]},{"role":"tool","tool_call_id":"call_a","content":"ok"}]}`
	for _, tc := range []struct {
		name, signer string
		wantCalls    int
	}{{"foreign", "other", 1}, {"unknown", "", 2}, {"own", "fake", 2}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{ctype: "application/json", reply: chatOK}
			setup(t, provider.Chat, f)
			if tc.signer != "" {
				noteSigner(sig, tc.signer)
			}
			f.refuse = func(b []byte) (int, string) {
				msgs := gjson.GetBytes(b, "messages")
				call := msgs.Get("0.tool_calls.0")
				if call.Get("id").String() != "call_a" || call.Get("function.name").String() != "read" || msgs.Get("1.tool_call_id").String() != "call_a" {
					t.Errorf("relay received unpaired call: %s", msgs)
				}
				extra := call.Get("extra_content")
				if extra.Get("google.other").Int() != 1 || !extra.Get("vendor_x").Bool() {
					t.Errorf("relay lost sibling extra_content fields: %s", extra)
				}
				if gotSig := extra.Get("google.thought_signature"); gotSig.Exists() {
					if tc.signer == "other" || gotSig.String() != sig {
						t.Errorf("relay received unexpected thought_signature: %s", extra)
					}
					return http.StatusBadRequest, studioCorrupted
				}
				return 0, ""
			}
			code, reply := post(t, "/v1/chat/completions", body)
			if code != http.StatusOK || !strings.Contains(reply, "hello") || f.calls != tc.wantCalls {
				t.Errorf("got HTTP %d after %d relay calls, want HTTP 200 after %d: %s", code, f.calls, tc.wantCalls, reply)
			}
		})
	}
}

func TestChatRelayWithoutSignaturesKeepsBody(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"plain", `{"messages": [ {"role":"assistant", "tool_calls":[{"id":"call_a", "type":"function", "function":{"name":"read", "arguments":"{}"}}]}, {"role":"tool", "tool_call_id":"call_a", "content":"ok"}]}`},
		{"field name in text only", `{"messages": [ {"role":"user", "content":"thought_signature"}, {"role":"assistant", "tool_calls":[{"id":"call_a", "type":"function", "function":{"name":"read", "arguments":"{}"}}]}, {"role":"tool", "tool_call_id":"call_a", "content":"ok"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(chatCallSignatures([]byte(tc.body), false, "relay", true)); got != tc.body {
				t.Errorf("unsigned body changed to %s", got)
			}
		})
	}
	// When extra_content holds only google.thought_signature, stripping drops
	// the empty container cleanly too.
	fresh(t)
	noteSigner("c2lnLW9ubHk=", "other")
	only := `{"messages":[{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"read","arguments":"{}"},"extra_content":{"google":{"thought_signature":"c2lnLW9ubHk="}}}]}]}`
	if out := string(chatCallSignatures([]byte(only), false, "relay", true)); strings.Contains(out, "extra_content") {
		t.Errorf("empty extra_content container kept: %s", out)
	}
	// Gemini-compatible write-back and signed-ID write-back both preserve
	// sibling extra_content fields while only touching google.thought_signature.
	noteSigner("c2lnLW93bg==", "relay")
	ownID := signedID("call_a", "c2lnLW93bg==")
	withSibling := `{"messages":[{"role":"assistant","tool_calls":[{"id":"` + ownID + `","type":"function","function":{"name":"read","arguments":"{}"},"extra_content":{"google":{"other":1},"vendor_x":true}}]}]}`
	for _, gemini := range []bool{false, true} {
		out := chatCallSignatures([]byte(withSibling), gemini, "relay", true)
		ec := gjson.GetBytes(out, "messages.0.tool_calls.0.extra_content")
		if ec.Get("google.thought_signature").String() != "c2lnLW93bg==" || ec.Get("google.other").Int() != 1 || !ec.Get("vendor_x").Bool() {
			t.Errorf("gemini=%v write-back lost sibling fields: %s", gemini, out)
		}
	}
}

// A signature goes back only to the upstream that gave it: a relay that
// gave none gets no extra_content, and the calls' own ids.
func TestRelaySignatureNotToAnother(t *testing.T) {
	fresh(t)
	const sig = "c2lnbmVkLWJ5LXBldGE="
	noteSigner(sig, "peta-gemini")
	id := signedID("call_a", sig)
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"` + id + `","type":"function","function":{"name":"a","arguments":"{}"}}]},{"role":"tool","tool_call_id":"` + id + `","content":"x"}]}`)
	if out := string(chatCallSignatures(body, false, "peta-gemini", true)); !strings.Contains(out, sig) || strings.Contains(out, sigMark) {
		t.Errorf("its signer was sent %s", out)
	}
	if out := string(chatCallSignatures(body, false, "other-relay", true)); strings.Contains(out, "extra_content") || strings.Contains(out, sigMark) {
		t.Errorf("another relay was sent %s", out)
	}
	r := &Request{Messages: []Message{
		{Role: "assistant", Parts: []Part{{Kind: ToolCall, ID: "call_a", Name: "a", Signature: sig}}},
		{Role: "user", Parts: []Part{{Kind: ToolResult, CallID: "call_a", Text: "x"}}},
	}}
	signer := *r
	signer.signer = "peta-gemini"
	if out := string(buildChat(&signer, "gemini-4-argon", "api.peta.example.com", false)); !strings.Contains(out, sig) {
		t.Errorf("its signer was sent %s", out)
	}
	other := *r
	other.signer = "other-relay"
	if out := string(buildChat(&other, "gemini-4-argon", "api.other.example.com", false)); strings.Contains(out, "extra_content") {
		t.Errorf("another relay was sent %s", out)
	}
}
