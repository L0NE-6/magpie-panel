package gateway

import (
	"bytes"

	"github.com/tidwall/gjson"
	"github.com/yetone/magpie/internal/provider"
)

// A reply relayed as the upstream sent it goes through no encoder, so a
// tool call naming no function in it reached the client (#7728, the
// #7565 fix's sibling): a Responses or Messages event stream, a body
// asked whole, and a Gemini reply either way. namelessRelay is the
// relay's own filter, beside chattidy.nameless which a Chat stream
// already had: an event that opens a call naming nothing ends the rest
// of it, the items after it are numbered over the gap the client would
// read, and the reply's end is mended when the calls it names were its
// only ones — a Messages message_delta is told end_turn, as the
// translated reply's stop is. A Gemini relay's request gets
// namelessGeminiOut, as Messages, Responses and Chat relayed requests
// already did. A call whose name member isn't there at all is read as
// naming nothing on the reply side too, though no case of that is known.
//
// The relay decodes no event it doesn't gate on: an event without the
// members a nameless call or its end shows in goes out as it came.

// namelessName is whether a gjson member names no function: not there,
// not a string, or "". One that isn't there at all is read as naming
// nothing, as namesNothing reads raw that won't decode.
func namelessName(n gjson.Result) bool {
	return !n.Exists() || n.Type != gjson.String || n.String() == ""
}

// namelessItem is whether a Responses output item is a call naming no
// function: a function_call or custom_tool_call with no name a client
// reads, as namelessItemsOut reads one in a request.
func namelessItem(it gjson.Result) bool {
	typ := it.Get("type").String()
	return (typ == "function_call" || typ == "custom_tool_call") && namelessName(it.Get("name"))
}

// namelessItemMap is namelessItem for a decoded output item, for the
// strip that already holds the reply as maps.
func namelessItemMap(it map[string]any) bool {
	typ, _ := it["type"].(string)
	if typ != "function_call" && typ != "custom_tool_call" {
		return false
	}
	name, ok := it["name"].(string)
	return !ok || name == ""
}

// namelessReply is whether a reply body could hold a nameless call for
// proto, the cheap gate before any decode, as emptyName is for a
// request.
func namelessReply(proto provider.Protocol, data []byte) bool {
	switch proto {
	case provider.Responses:
		return bytes.Contains(data, []byte("output"))
	case provider.Anthropic:
		return bytes.Contains(data, []byte("tool_use")) || bytes.Contains(data, []byte(`"stop_reason"`))
	case provider.Gemini:
		return bytes.Contains(data, []byte("functionCall"))
	}
	return false
}

// namelessRelay is the relayed reply's filter (see the top of the file).
// sse for an event stream; a reply that came whole is held to its flush
// and stripped there, as the tidies around it do.
type namelessRelay struct {
	proto provider.Protocol
	sse   bool
	buf   []byte
	drops map[int64]bool // the output items or content blocks let none of
	named bool           // a Messages reply that kept a call naming a function
}

func (t *namelessRelay) write(b []byte) []byte {
	t.buf = append(t.buf, b...)
	if !t.sse {
		return nil
	}
	out, rest := tidyEvents(t.buf, t.event)
	t.buf = rest
	return out
}

func (t *namelessRelay) flush() []byte {
	b := t.buf
	t.buf = nil
	if !t.sse {
		switch t.proto {
		case provider.Responses:
			return withoutNamelessResponse(b)
		case provider.Anthropic:
			return withoutNamelessBlocksReply(b)
		case provider.Gemini:
			return namelessGeminiReply(b)
		}
		return b
	}
	if len(b) == 0 {
		return nil
	}
	// the event the upstream began and never ended is read as a whole
	// one would be: its call's later half can't come, so the start of
	// one naming nothing is no call either
	return t.event(b)
}

func (t *namelessRelay) event(ev []byte) []byte {
	data := eventData(ev)
	// once a call is dropped, every event is read: one still naming its
	// index ends with it, one coming after is numbered over the gap
	if len(data) == 0 || len(t.drops) == 0 && !namelessReply(t.proto, data) {
		return ev
	}
	switch t.proto {
	case provider.Responses:
		return t.responsesEvent(ev, data)
	case provider.Anthropic:
		return t.messagesEvent(ev, data)
	case provider.Gemini:
		return t.geminiEvent(ev, data)
	}
	return ev
}

// dropsBefore is how many dropped output items or content blocks come
// before i: the amount the index the client reads moves down by.
func (t *namelessRelay) dropsBefore(i int64) int {
	n := 0
	for d := range t.drops {
		if d < i {
			n++
		}
	}
	return n
}

// renumber is ev with its key member (output_index or index) put at its
// value minus the drops before it, so the client reads the kept items
// numbered over the gap a dropped call left.
func (t *namelessRelay) renumber(ev, data []byte, e gjson.Result, key string) []byte {
	at := e.Get(key)
	if !at.Exists() {
		return ev
	}
	i := at.Int()
	if t.dropsBefore(i) == 0 {
		return ev
	}
	var m map[string]any
	if decodeNumbers(data, &m) != nil {
		return ev
	}
	m[key] = i - int64(t.dropsBefore(i))
	nb, err := marshalPlain(m)
	if err != nil {
		return ev
	}
	return withData(ev, nb)
}

// responsesEvent is one Responses event through the filter: an output
// item that opens a call naming nothing marks its index dropped, every
// event of one ends with it, and the reply's last event lists its output
// without it.
func (t *namelessRelay) responsesEvent(ev, data []byte) []byte {
	if !gjson.ValidBytes(data) {
		return ev
	}
	e := gjson.ParseBytes(data)
	at := e.Get("output_index")
	if at.Exists() && t.drops[at.Int()] {
		return nil
	}
	typ := e.Get("type").String()
	switch {
	case typ == "response.output_item.added" || typ == "response.output_item.done":
		if !at.Exists() || !namelessItem(e.Get("item")) {
			break
		}
		if t.drops == nil {
			t.drops = map[int64]bool{}
		}
		t.drops[at.Int()] = true
		return nil
	case typ == "response.completed" || typ == "response.incomplete" || typ == "response.failed":
		seen := false
		for _, it := range e.Get("response.output").Array() {
			if namelessItem(it) {
				seen = true
				break
			}
		}
		if !seen {
			break
		}
		var m map[string]any
		if decodeNumbers(data, &m) != nil {
			return ev
		}
		res, _ := m["response"].(map[string]any)
		items, _ := res["output"].([]any)
		kept := make([]any, 0, len(items))
		for _, v := range items {
			if it, _ := v.(map[string]any); it == nil || !namelessItemMap(it) {
				kept = append(kept, v)
			}
		}
		res["output"] = kept
		nb, err := marshalPlain(m)
		if err != nil {
			return ev
		}
		return withData(ev, nb)
	}
	if len(t.drops) == 0 {
		return ev
	}
	return t.renumber(ev, data, e, "output_index")
}

// messagesEvent is one Messages event through the filter: a tool_use
// block naming nothing marks its index dropped, its deltas and stop end
// with it, the blocks after are numbered over the gap, and a message
// that stopped for tools with none left is told it ended.
func (t *namelessRelay) messagesEvent(ev, data []byte) []byte {
	if !gjson.ValidBytes(data) {
		return ev
	}
	e := gjson.ParseBytes(data)
	at := e.Get("index")
	if at.Exists() && t.drops[at.Int()] {
		return nil
	}
	switch e.Get("type").String() {
	case "content_block_start":
		cb := e.Get("content_block")
		if cb.Get("type").String() != "tool_use" {
			break
		}
		if namelessName(cb.Get("name")) {
			if at.Exists() {
				if t.drops == nil {
					t.drops = map[int64]bool{}
				}
				t.drops[at.Int()] = true
			}
			return nil
		}
		t.named = true
	case "message_delta":
		if e.Get("delta.stop_reason").String() != "tool_use" || t.named {
			break
		}
		var m map[string]any
		if decodeNumbers(data, &m) != nil {
			return ev
		}
		delta, ok := m["delta"].(map[string]any)
		if !ok {
			return ev
		}
		delta["stop_reason"] = "end_turn"
		nb, err := marshalPlain(m)
		if err != nil {
			return ev
		}
		return withData(ev, nb)
	}
	if len(t.drops) == 0 || !at.Exists() {
		return ev
	}
	return t.renumber(ev, data, e, "index")
}

// geminiEvent is one Gemini event through the filter: its candidates'
// functionCall parts naming nothing go, as a whole reply's do. An event
// with none goes as it came.
func (t *namelessRelay) geminiEvent(ev, data []byte) []byte {
	nb := namelessGeminiReply(data)
	if bytes.Equal(nb, data) {
		return ev
	}
	return withData(ev, nb)
}

// withoutNamelessResponse is a Responses reply that came whole without
// the calls in its output that name no function.
func withoutNamelessResponse(b []byte) []byte {
	if !gjson.ValidBytes(b) {
		return b
	}
	seen := false
	for _, it := range gjson.GetBytes(b, "output").Array() {
		if namelessItem(it) {
			seen = true
			break
		}
	}
	if !seen {
		return b
	}
	var m map[string]any
	if decodeNumbers(b, &m) != nil {
		return b
	}
	items, _ := m["output"].([]any)
	kept := make([]any, 0, len(items))
	for _, v := range items {
		if it, _ := v.(map[string]any); it == nil || !namelessItemMap(it) {
			kept = append(kept, v)
		}
	}
	m["output"] = kept
	nb, err := marshalPlain(m)
	if err != nil {
		return b
	}
	return nb
}

// withoutNamelessBlocksReply is a Messages reply that came whole without
// the tool_use blocks that name no function, told it ended when those
// were its only calls.
func withoutNamelessBlocksReply(b []byte) []byte {
	if !gjson.ValidBytes(b) {
		return b
	}
	content := gjson.GetBytes(b, "content")
	seen, named := false, false
	for _, bl := range content.Array() {
		if bl.Get("type").String() != "tool_use" {
			continue
		}
		if namelessName(bl.Get("name")) {
			seen = true
		} else {
			named = true
		}
	}
	if !seen {
		return b
	}
	var m map[string]any
	if decodeNumbers(b, &m) != nil {
		return b
	}
	blocks, _ := m["content"].([]any)
	kept := make([]any, 0, len(blocks))
	for _, v := range blocks {
		bl, _ := v.(map[string]any)
		name, ok := bl["name"].(string)
		if bl["type"] == "tool_use" && (!ok || name == "") {
			continue
		}
		kept = append(kept, v)
	}
	m["content"] = kept
	if !named && m["stop_reason"] == "tool_use" {
		m["stop_reason"] = "end_turn"
	}
	nb, err := marshalPlain(m)
	if err != nil {
		return b
	}
	return nb
}

// namelessGeminiReply is a Gemini reply, one event's data or a whole
// body, without the functionCall parts that name no function. A content
// left with no parts keeps none.
func namelessGeminiReply(b []byte) []byte {
	if !gjson.ValidBytes(b) {
		return b
	}
	seen := false
	for _, c := range gjson.GetBytes(b, "candidates").Array() {
		for _, p := range c.Get("content.parts").Array() {
			if p.Get("functionCall").Exists() && namelessName(p.Get("functionCall.name")) {
				seen = true
			}
		}
	}
	if !seen {
		return b
	}
	var m map[string]any
	if decodeNumbers(b, &m) != nil {
		return b
	}
	cands, _ := m["candidates"].([]any)
	for _, v := range cands {
		c, _ := v.(map[string]any)
		content, _ := c["content"].(map[string]any)
		if content == nil {
			continue
		}
		parts, _ := content["parts"].([]any)
		kept := make([]any, 0, len(parts))
		for _, pv := range parts {
			p, _ := pv.(map[string]any)
			fc, _ := p["functionCall"].(map[string]any)
			name, _ := fc["name"].(string)
			if fc != nil && name == "" {
				continue
			}
			kept = append(kept, pv)
		}
		if len(kept) == 0 {
			delete(content, "parts")
		} else {
			content["parts"] = kept
		}
	}
	nb, err := marshalPlain(m)
	if err != nil {
		return b
	}
	return nb
}

// namelessGeminiOut is a Gemini request, relayed as the client wrote it,
// without the functionCall and functionResponse parts that name no
// function — the call and the answer to it go together. A content left
// with no parts is left out of contents; a request left with none goes
// as it came, so an empty contents never reaches the upstream. A body
// with none comes back byte for byte.
func namelessGeminiOut(body []byte) []byte {
	if !emptyName.Match(body) || !bodyHasBytes(body, []byte("function")) {
		return body
	}
	raw, found, ok := topMember(body, "contents")
	if !ok || !found {
		return body
	}
	var contents []map[string]any
	if decodeNumbers(raw, &contents) != nil {
		return body
	}
	seen := false
	out := make([]map[string]any, 0, len(contents))
	for _, c := range contents {
		parts, _ := c["parts"].([]any)
		kept := make([]any, 0, len(parts))
		for _, pv := range parts {
			p, _ := pv.(map[string]any)
			drop := false
			for _, kind := range []string{"functionCall", "functionResponse"} {
				f, _ := p[kind].(map[string]any)
				name, _ := f["name"].(string)
				if f != nil && name == "" {
					drop = true
				}
			}
			if drop {
				seen = true
				continue
			}
			kept = append(kept, pv)
		}
		if len(kept) == 0 {
			seen = true
			continue
		}
		c["parts"] = kept
		out = append(out, c)
	}
	if !seen || len(out) == 0 {
		return body
	}
	arr, err := marshalPlain(out)
	if err != nil {
		return body
	}
	return withTopValue(body, "contents", arr)
}
