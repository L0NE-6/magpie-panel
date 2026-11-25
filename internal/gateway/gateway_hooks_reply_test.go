package gateway

import (
	"bytes"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// A Responses stream whose response.completed names an empty output still
// hands the hook the output the client got: the items the stream's
// output_item.done events carried (#7760). response.incomplete is a reply
// too, and an event type written only on the event: line is read.
func TestGatewayReplyKeepsStreamedOutput(t *testing.T) {
	stream := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r1\",\"model\":\"m\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\",\"annotations\":[]}]}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":5,\"output_tokens\":3}}}\n\n"
	got, err := gatewayReply(provider.Responses, []byte(stream))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(`"text":"hello"`)) {
		t.Errorf("the streamed output was lost: %s", got)
	}
	if !bytes.Contains(got, []byte(`"output_tokens":3`)) {
		t.Errorf("the completed response's usage was lost: %s", got)
	}
	// a completed response that already carries its output is kept as it is
	whole := sse(
		`data: {"type":"response.completed","response":{"id":"r2","model":"m","status":"completed","output":[{"type":"message","id":"msg_2","content":[{"type":"output_text","text":"kept"}]}]}}`)
	got, err = gatewayReply(provider.Responses, []byte(whole))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(`"text":"kept"`)) {
		t.Errorf("an output the completed response carried was lost: %s", got)
	}
}

// response.incomplete is the reply a stream cut short ends with: the hook
// gets its id, model, status and usage, with the streamed output filled in
// (#7760).
func TestGatewayReplyTakesIncomplete(t *testing.T) {
	stream := sse(
		`data: {"type":"response.output_text.delta","delta":"partial"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","status":"incomplete","content":[{"type":"output_text","text":"partial","annotations":[]}]}}`,
		`data: {"type":"response.incomplete","response":{"id":"r1","model":"m","status":"incomplete","output":[],"usage":{"output_tokens":2}}}`)
	got, err := gatewayReply(provider.Responses, []byte(stream))
	if err != nil {
		t.Fatalf("an incomplete stream was refused: %v", err)
	}
	for _, want := range []string{`"status":"incomplete"`, `"text":"partial"`, `"output_tokens":2`, `"id":"r1"`} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("%s was lost: %s", want, got)
		}
	}
	// the type written only on the event: line is the same reply
	stream = "event: response.incomplete\ndata: {\"response\":{\"id\":\"r2\",\"model\":\"m\",\"status\":\"incomplete\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"there\"}]}]}}\n\n"
	got, err = gatewayReply(provider.Responses, []byte(stream))
	if err != nil {
		t.Fatalf("an event type on the event: line was refused: %v", err)
	}
	if !bytes.Contains(got, []byte(`"text":"there"`)) {
		t.Errorf("the reply was lost: %s", got)
	}
}
