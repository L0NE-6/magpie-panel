package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/redact"
	"github.com/yetone/magpie/internal/settings"
)

func TestCaptureRequestBodyLimit(t *testing.T) {
	body := []byte(strings.Repeat("x", callBodyLimit+17))
	got, truncated := captureRequestBody(body)
	if len(got) != callBodyLimit || !truncated {
		t.Fatalf("captured %d bytes, truncated=%v", len(got), truncated)
	}
	for _, n := range []int{0, 5, callBodyLimit} {
		in := []byte(strings.Repeat("y", n))
		if got, truncated := captureRequestBody(in); got != string(in) || truncated {
			t.Fatalf("%d bytes captured as %d, truncated=%v", n, len(got), truncated)
		}
	}
	// copied once: the capture is all a request's capture allocates
	if n := allocated(func() { captureRequestBody(body) }); n > callBodyLimit*5/4 {
		t.Fatalf("capturing %d bytes allocated %d", callBodyLimit, n)
	}
	// and it is a copy: the body's memory is not the capture's
	body[0] = 'z'
	if got[0] != 'x' {
		t.Fatal("the capture reads the body's memory")
	}
}

func TestCaptureResponseWriterPreservesResponse(t *testing.T) {
	r := httptest.NewRecorder()
	w := &captureResponseWriter{ResponseWriter: r}
	_, _ = w.Write([]byte(`{"ok":true}`))
	if got := w.body.text(); got != `{"ok":true}` {
		t.Fatalf("capture = %q", got)
	}
	if got := r.Body.String(); got != `{"ok":true}` {
		t.Fatalf("response = %q", got)
	}
}

// A body the gateway's OTLP export carries loses a user rule's key while
// Mask secrets is on, and keeps it while the rule is off.
func TestBodyForExportScrubsTheUsersRuleWhenSecretsAreMasked(t *testing.T) {
	fresh(t)
	const key = "rz_ExportKey1234567"
	body := `{"choices":[{"message":{"content":"using ` + key + `"}}]}`
	if err := settings.Save(settings.Settings{Redact: true,
		RedactRules: []redact.Rule{{Kind: "RELAY", Prefix: "rz_"}}}); err != nil {
		t.Fatal(err)
	}
	if got := bodyForExport(body, false); strings.Contains(got, key) || !strings.Contains(got, "[REDACTED:RELAY]") {
		t.Errorf("masking on, the exported body: %s", got)
	}
	if err := settings.Save(settings.Settings{
		RedactRules: []redact.Rule{{Kind: "RELAY", Prefix: "rz_"}}}); err != nil {
		t.Fatal(err)
	}
	if got := bodyForExport(body, false); !strings.Contains(got, key) {
		t.Errorf("masking off, the exported body lost the key: %s", got)
	}
}
