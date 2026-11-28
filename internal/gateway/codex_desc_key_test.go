package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/access"
)

// A gateway key held to some models (#882) is refused a thread description
// by any other, as it is a commit message: the model a description names is
// the caller's, so magpieChose must not stand in for the key's allowance
// (#8012).
func TestGatewayKeyHoldsAThreadDescription(t *testing.T) {
	fresh(t)
	plan := &fake{t: t, ctype: "application/json", reply: `{"id":"from-plan","choices":[]}`}
	spare := &fake{t: t, ctype: "application/json", reply: `{"id":"from-spare","choices":[]}`}
	twoProviders(t, plan, spare)
	keys, secrets := newCaller(t, "Held")
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{"plan/*"}}); err != nil {
		t.Fatal(err)
	}
	h := New().Handler()
	desc := func(model string) *httptest.ResponseRecorder {
		body := `{"model":"` + model + `","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Describe this thread"}]}]}`
		r := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+secrets[0])
		r.Header.Set("session_id", "description-thread")
		r.Header.Set("x-codex-turn-metadata", `{"thread_source":"thread_description"}`)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := desc("spare/m2"); w.Code < 400 {
		t.Fatalf("a description on spare/m2 answered %d for a key held to plan/*: %s", w.Code, w.Body.String())
	}
	if plan.calls != 0 || spare.calls != 0 {
		t.Fatalf("a provider was asked for a model the key may not use: plan %d spare %d", plan.calls, spare.calls)
	}
}
