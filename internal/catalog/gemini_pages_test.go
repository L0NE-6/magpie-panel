package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A Gemini model list that never ends is partial, and a partial list is not a
// list: returning it as one drops every model past the cap from a provider's
// picks (#8030).
func TestFetchGeminiRefusesAPartialList(t *testing.T) {
	for _, c := range []struct {
		name  string
		page  func(u string, signed string) (string, string) // body, next
		about string
	}{
		{
			name: "always another page",
			page: func(_, token string) (string, string) {
				// the token always moves on, so 20 pages are read and more remain
				next := "p" + token + "x"
				if token == "" {
					next = "p1"
				}
				return `{"models":[{"name":"models/m` + next + `"}]}`, next
			},
			about: "still paging",
		},
		{
			name: "the same token again",
			page: func(_, token string) (string, string) {
				if token == "" {
					token = "repeated"
				}
				return `{"models":[{"name":"models/m1"}]}`, token
			},
			about: "asks again",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				token := r.URL.Query().Get("pageToken")
				body, next := c.page(r.URL.Path, token)
				if next != "" {
					var m map[string]any
					_ = json.Unmarshal([]byte(body), &m)
					m["nextPageToken"] = next
					body = mustJSON(m)
				}
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			_, _, err := FetchGemini(context.Background(), srv.URL, "k", nil)
			if err == nil {
				t.Fatalf("a list that never ends was returned as a whole one")
			}
			if !strings.Contains(err.Error(), c.about) {
				t.Fatalf("err %q does not say %q", err, c.about)
			}
		})
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
