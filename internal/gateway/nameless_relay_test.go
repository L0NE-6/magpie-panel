package gateway

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// A Responses reply relayed as the upstream sent it hands the client no
// call naming no function, as a translated reply doesn't (#7728): the
// item opening one ends its arguments and done with it, the items after
// are numbered over the gap, and the reply's last event lists its output
// without it.
func TestNamelessRelayResponsesStream(t *testing.T) {
	stream := []byte(sse(
		`data: {"type":"response.created","response":{"id":"r1","model":"m"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_bad","call_id":"call_bad","name":"","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_bad","delta":"{}"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_bad","call_id":"call_bad","name":"","arguments":"{}"}}`,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_ok","call_id":"call_ok","name":"Read","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"item_id":"fc_ok","delta":"{}"}`,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_ok","call_id":"call_ok","name":"Read","arguments":"{}"}}`,
		`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[{"type":"function_call","id":"fc_bad","call_id":"call_bad","name":"","arguments":"{}"},{"type":"function_call","id":"fc_ok","call_id":"call_ok","name":"Read","arguments":"{}"}],"usage":{"input_tokens":5,"output_tokens":5}}}`))

	// split mid-token and mid-frame: the filter sees reads as they come
	got := []byte{}
	nr := &namelessRelay{proto: provider.Responses, sse: true}
	for _, part := range [][]byte{stream[:37], stream[37:200], stream[200:201], stream[201:]} {
		got = append(got, nr.write(part)...)
	}
	got = append(got, nr.flush()...)
	for _, dead := range []string{"fc_bad", "call_bad", `"output_index":1`} {
		if bytes.Contains(got, []byte(dead)) {
			t.Errorf("%q is still in the relayed reply: %s", dead, got)
		}
	}
	for _, kept := range []string{"fc_ok", "call_ok", "Read", `"output_index":0`, "response.completed"} {
		if !bytes.Contains(got, []byte(kept)) {
			t.Errorf("%q was lost: %s", kept, got)
		}
	}
	if n := bytes.Count(got, []byte("function_call_arguments.delta")); n != 1 {
		t.Errorf("the dropped call's argument fragments went out: %d deltas, %s", n, got)
	}
	if n := bytes.Count(got, []byte("output_item.done")); n != 1 {
		t.Errorf("the dropped call's done went out: %d dones, %s", n, got)
	}
	// a reply with no nameless call goes out as it came
	nr = &namelessRelay{proto: provider.Responses, sse: true}
	plain := []byte(sse(`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_ok","call_id":"call_ok","name":"Read","arguments":""}}`))
	if out := append(nr.write(plain), nr.flush()...); !bytes.Equal(out, plain) {
		t.Errorf("a reply with no nameless call changed: %s", out)
	}
}

// A Messages reply relayed as the upstream sent it hands no tool_use
// naming no function: the block opening one ends its deltas and stop
// with it, the blocks after are numbered over the gap, and a message
// that stopped for tools with none left is told it ended.
func TestNamelessRelayMessagesStream(t *testing.T) {
	stream := []byte(sse(
		`data: {"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_bad","name":"","input":{}}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"done"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		`data: {"type":"message_stop"}`))

	got := []byte{}
	nr := &namelessRelay{proto: provider.Anthropic, sse: true}
	for _, part := range [][]byte{stream[:50], stream[50:400], stream[400:401], stream[401:]} {
		got = append(got, nr.write(part)...)
	}
	got = append(got, nr.flush()...)
	for _, dead := range []string{"call_bad", "tool_use", `"index":1`} {
		if bytes.Contains(got, []byte(dead)) {
			t.Errorf("%q is still in the relayed reply: %s", dead, got)
		}
	}
	for _, kept := range []string{"text_delta", "done", `"index":0`, `"stop_reason":"end_turn"`, "message_stop"} {
		if !bytes.Contains(got, []byte(kept)) {
			t.Errorf("%q was lost: %s", kept, got)
		}
	}
	if n := bytes.Count(got, []byte("input_json_delta")); n != 0 {
		t.Errorf("the dropped block's deltas went out: %d, %s", n, got)
	}
	if n := bytes.Count(got, []byte("content_block_stop")); n != 1 {
		t.Errorf("the dropped block's stop went out: %d stops, %s", n, got)
	}
	// a kept call naming a function keeps the tool_use stop
	stream = []byte(sse(
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_ok","name":"Read","input":{}}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`))
	nr = &namelessRelay{proto: provider.Anthropic, sse: true}
	if out := append(nr.write(stream), nr.flush()...); !bytes.Equal(out, stream) {
		t.Errorf("a reply whose calls all name a function changed: %s", out)
	}
}

// Replies that came whole are stripped the same way, on each API.
func TestNamelessRelayWholeReplies(t *testing.T) {
	for _, c := range []struct {
		proto provider.Protocol
		body  string
		dead  []string
		kept  []string
	}{
		{provider.Responses,
			`{"id":"r1","status":"completed","output":[{"type":"function_call","call_id":"call_bad","name":"","arguments":"{}"},{"type":"function_call","call_id":"call_ok","name":"Read","arguments":"{}"}]}`,
			[]string{"call_bad"}, []string{"call_ok", "Read"}},
		{provider.Anthropic,
			`{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"call_bad","name":"","input":{}},{"type":"text","text":"done"}],"stop_reason":"tool_use"}`,
			[]string{"call_bad", "tool_use"}, []string{"done", `"stop_reason":"end_turn"`}},
		{provider.Gemini,
			`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"","args":{}}},{"text":"ok"}]},"finishReason":"STOP"}]}`,
			[]string{"functionCall"}, []string{"ok", "STOP"}},
	} {
		nr := &namelessRelay{proto: c.proto, sse: false}
		got := append(nr.write([]byte(c.body)), nr.flush()...)
		for _, dead := range c.dead {
			if bytes.Contains(got, []byte(dead)) {
				t.Errorf("%s: %q is still in the whole reply: %s", c.proto, dead, got)
			}
		}
		for _, kept := range c.kept {
			if !bytes.Contains(got, []byte(kept)) {
				t.Errorf("%s: %q was lost: %s", c.proto, kept, got)
			}
		}
	}
}

// A Gemini request relayed as the client wrote it goes without the
// functionCall and functionResponse parts that name no function — the
// call and the response to it go together — the rest as it came.
func TestNamelessGeminiOut(t *testing.T) {
	ask := `{"contents":[
		{"role":"user","parts":[{"text":"look at /x"}]},
		{"role":"model","parts":[{"functionCall":{"name":"","args":{}}},{"functionCall":{"name":"Read","args":{"file_path":"/x"}}}]},
		{"role":"user","parts":[{"functionResponse":{"name":"","response":{"out":0}}},{"functionResponse":{"name":"Read","response":{"out":1}}}]},
		{"role":"user","parts":[{"text":"continue"}]}]}`
	got := namelessGeminiOut([]byte(ask))
	for _, dead := range []string{`"name":""`} {
		if bytes.Contains(got, []byte(dead)) {
			t.Errorf("%q is still in the relayed request: %s", dead, got)
		}
	}
	for _, kept := range []string{"Read", "look at /x", "continue"} {
		if !bytes.Contains(got, []byte(kept)) {
			t.Errorf("%q was lost: %s", kept, got)
		}
	}
	named := []byte(strings.Replace(ask, `"name":""`, `"name":"Grep"`, 2))
	if again := namelessGeminiOut(named); !bytes.Equal(again, named) {
		t.Errorf("a request with no nameless part changed: %s", again)
	}
}

// namelessReplyVendor answers on one API a reply with a call naming no
// function beside a named one or text, for the relay tests: an SSE
// stream as each API's upstream speaks it.
type namelessReplyVendor struct {
	proto provider.Protocol
	mu    sync.Mutex
	got   []byte
}

func (v *namelessReplyVendor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	v.mu.Lock()
	v.got = b
	v.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	switch v.proto {
	case provider.Responses:
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"m"}}`,
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_bad","call_id":"call_bad","name":"","arguments":""}}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_bad","call_id":"call_bad","name":"","arguments":"{}"}}`,
			`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_ok","call_id":"call_ok","name":"Read","arguments":"{}"}}`,
			`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_ok","call_id":"call_ok","name":"Read","arguments":"{}"}}`,
			`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[{"type":"function_call","id":"fc_bad","call_id":"call_bad","name":"","arguments":"{}"},{"type":"function_call","id":"fc_ok","call_id":"call_ok","name":"Read","arguments":"{}"}],"usage":{"input_tokens":5,"output_tokens":5}}}`))
	case provider.Gemini:
		io.WriteString(w, sse(
			`data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"","args":{}}},{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":5}}`))
	default:
		io.WriteString(w, sse(
			`data: {"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_bad","name":"","input":{}}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
			`data: {"type":"content_block_stop","index":0}`,
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_ok","name":"Read","input":{"file_path":"/x"}}}`,
			`data: {"type":"content_block_stop","index":1}`,
			`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
			`data: {"type":"message_stop"}`))
	}
}

func (v *namelessReplyVendor) last() []byte {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.got
}

// End to end on a Responses relay: the upstream's own reply with a call
// naming no function hands the client none of it — not its item, not its
// arguments, not its place in the reply's output.
func TestNamelessRelayResponsesUpstream(t *testing.T) {
	fresh(t)
	up := &namelessReplyVendor{proto: provider.Responses}
	vendor := httptest.NewServer(up)
	t.Cleanup(vendor.Close)
	if err := provider.Save(provider.Provider{ID: "cc", Name: "CC", Key: "k", Responses: vendor.URL + "/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/v1/responses", "application/json", strings.NewReader(`{"model":"cc/m","stream":true,"input":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	reply, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, reply)
	}
	for _, dead := range []string{"fc_bad", "call_bad", `"output_index":1`} {
		if bytes.Contains(reply, []byte(dead)) {
			t.Errorf("%q reached the client: %s", dead, reply)
		}
	}
	for _, kept := range []string{"fc_ok", "call_ok", "Read"} {
		if !bytes.Contains(reply, []byte(kept)) {
			t.Errorf("%q was lost: %s", kept, reply)
		}
	}
}

// End to end on a Messages relay: the nameless block and its deltas
// never reach the client, the blocks after are renumbered, and the reply
// still stops for the tool that names its function.
func TestNamelessRelayMessagesUpstream(t *testing.T) {
	fresh(t)
	up := &namelessReplyVendor{proto: provider.Anthropic}
	vendor := httptest.NewServer(up)
	t.Cleanup(vendor.Close)
	if err := provider.Save(provider.Provider{ID: "cc", Name: "CC", Key: "k", Anthropic: vendor.URL, Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(`{"model":"cc/m","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	reply, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, reply)
	}
	for _, dead := range []string{"call_bad", `"index":1`} {
		if bytes.Contains(reply, []byte(dead)) {
			t.Errorf("%q reached the client: %s", dead, reply)
		}
	}
	for _, kept := range []string{"call_ok", "Read", `"index":0`} {
		if !bytes.Contains(reply, []byte(kept)) {
			t.Errorf("%q was lost: %s", kept, reply)
		}
	}
}

// End to end on a Gemini relay, both ways: a nameless functionCall and
// the functionResponse to it in the request never reach the upstream,
// and the upstream's nameless functionCall never reaches the client.
func TestNamelessRelayGeminiBothWays(t *testing.T) {
	fresh(t)
	up := &namelessReplyVendor{proto: provider.Gemini}
	vendor := httptest.NewServer(up)
	t.Cleanup(vendor.Close)
	if err := provider.Save(provider.Provider{ID: "cc", Name: "CC", Key: "k", Gemini: vendor.URL, Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New().Handler())
	t.Cleanup(srv.Close)
	ask := `{"contents":[
		{"role":"model","parts":[{"functionCall":{"name":"","args":{}}},{"functionCall":{"name":"Read","args":{"file_path":"/x"}}}]},
		{"role":"user","parts":[{"functionResponse":{"name":"","response":{"out":0}}},{"functionResponse":{"name":"Read","response":{"out":1}}}]},
		{"role":"user","parts":[{"text":"continue"}]}]}`
	res, err := http.Post(srv.URL+"/v1beta/models/cc/m:streamGenerateContent", "application/json", strings.NewReader(ask))
	if err != nil {
		t.Fatal(err)
	}
	reply, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, reply)
	}
	sent := up.last()
	if len(sent) == 0 {
		t.Fatal("no request reached the upstream")
	}
	for _, dead := range []string{`"name":""`} {
		if bytes.Contains(sent, []byte(dead)) {
			t.Errorf("%q reached the upstream: %s", dead, sent)
		}
	}
	for _, kept := range []string{"Read", "continue"} {
		if !bytes.Contains(sent, []byte(kept)) {
			t.Errorf("%q was lost: %s", kept, sent)
		}
	}
	if bytes.Contains(reply, []byte("functionCall")) {
		t.Errorf("the client was handed a call naming nothing: %s", reply)
	}
	if !bytes.Contains(reply, []byte("ok")) {
		t.Errorf("the reply's text was lost: %s", reply)
	}
}
