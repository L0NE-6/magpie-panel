package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/settings"
)

// #7748: three agy-cli accounts, only the user's own ~/.gemini account
// listing the premium models. The provider must still route those models
// to that account, not to the bridge accounts that lack them.
func TestAgyOwnAccountRoutesModelsOnlyItLists(t *testing.T) {
	claudeHome(t)
	t.Setenv("MAGPIE_BUN", "")
	spec := "@magpie-community/opencode-agy-cli-auth"
	write := func(name string, v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		os.MkdirAll(settings.Dir(), 0o700)
		if err := os.WriteFile(filepath.Join(settings.Dir(), name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("plugins.json", map[string]any{"plugins": []map[string]any{{"spec": spec}}})
	write("plugin-auth.json", map[string]map[string]any{
		"agy-cli":   {"type": "oauth", "access": "own-access", "refresh": "own-refresh", "expires": 4102444800000, "metadata": map[string]any{"email": "own@example.com"}},
		"agy-cli#a": {"type": "oauth", "access": "a-access", "refresh": "a-refresh", "expires": 4102444800000, "metadata": map[string]any{"email": "a@example.com", "agyHome": "aaaaaaaa"}},
		"agy-cli#b": {"type": "oauth", "access": "b-access", "refresh": "b-refresh", "expires": 4102444800000, "metadata": map[string]any{"email": "b@example.com", "agyHome": "bbbbbbbb"}},
	})
	premium := []string{"gemini-3.8-flash", "gemini-4-argon", "gemini-4-carbon", "gemini-4-neon", "claude-sonnet-5-5"}
	plugin.UseCached([]plugin.Provider{{
		ID: "agy-cli", Spec: spec, Name: "Antigravity (local agy CLI)",
		Methods: []plugin.Method{{Type: "oauth", Label: "Sign in"}},
		Accounts: []plugin.Account{
			{Key: "agy-cli", Type: "oauth", AccountID: "own@example.com", Models: premium},
			{Key: "agy-cli#a", Type: "oauth", AccountID: "a@example.com", Models: []string{"gemini-3.8-flash"}},
			{Key: "agy-cli#b", Type: "oauth", AccountID: "b@example.com", Models: []string{"gemini-3.8-flash"}},
		},
		Models: listedModels(t, `[
			{"id":"gemini-3.8-flash","name":"Gemini 3.8 Flash"},
			{"id":"gemini-4-argon","name":"Gemini 4 Argon"},
			{"id":"gemini-4-carbon","name":"Gemini 4 Carbon"},
			{"id":"gemini-4-neon","name":"Gemini 4 Neon"},
			{"id":"claude-sonnet-5-5","name":"Claude Sonnet 5.5"}
		]`),
	}})
	t.Cleanup(func() { plugin.UseCached(nil) })
	saveLogins(t,
		savedLogin{Agent: "plugin:agy-cli", User: "a@example.com", Home: "agy-cli#a", On: true, Order: 1},
		savedLogin{Agent: "plugin:agy-cli", User: "b@example.com", Home: "agy-cli#b", On: true, Order: 2},
		savedLogin{Agent: "plugin:agy-cli", User: "own@example.com", Home: "agy-cli", On: true, Order: 3},
	)

	var pp Provider
	for _, p := range pluginAccounts() {
		if p.ID == "agy-cli" {
			pp = p
			break
		}
	}
	if pp.ID == "" {
		t.Fatal("agy-cli provider is missing")
	}
	out, left, barred := pp.AccountsFor("gemini-4-argon", nil)
	if len(out) != 1 || out[0].Account.pluginKey != "agy-cli" {
		t.Fatalf("gemini-4-argon goes to %+v (left %+v, barred %+v), want only the own account", out, left, barred)
	}
	if len(left) != 2 {
		t.Fatalf("bridge accounts that lack gemini-4-argon were not left behind: %+v", left)
	}
	out, left, barred = pp.AccountsFor("gemini-3.8-flash", nil)
	if len(out) != 3 || len(left) != 0 || len(barred) != 0 {
		t.Fatalf("gemini-3.8-flash goes to %+v (left %+v, barred %+v), want all three accounts", out, left, barred)
	}
	keys := []string{}
	for _, a := range out {
		keys = append(keys, a.Account.pluginKey)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"agy-cli", "agy-cli#a", "agy-cli#b"}) {
		t.Fatalf("accounts for a shared model: %v", keys)
	}
}

// The agy bridge lists effort variants (`gemini-4-argon-high`) while
// Magpie routes the family id (`gemini-4-argon`). A bridge account whose
// list fell back serves all models; the own account must still match its
// raw family variants, or it is left out after the fallback accounts.
func TestAgyPluginFamilyListsRawEffortVariants(t *testing.T) {
	claudeHome(t)
	t.Setenv("MAGPIE_BUN", "")
	spec := "@magpie-community/opencode-agy-cli-auth"
	write := func(name string, v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		os.MkdirAll(settings.Dir(), 0o700)
		if err := os.WriteFile(filepath.Join(settings.Dir(), name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("plugins.json", map[string]any{"plugins": []map[string]any{{"spec": spec}}})
	write("plugin-auth.json", map[string]map[string]any{
		"agy-cli":   {"type": "oauth", "access": "own-access", "refresh": "own-refresh", "expires": 4102444800000},
		"agy-cli#a": {"type": "oauth", "access": "a-access", "refresh": "a-refresh", "expires": 4102444800000, "metadata": map[string]any{"agyHome": "aaaaaaaa"}},
		"agy-cli#b": {"type": "oauth", "access": "b-access", "refresh": "b-refresh", "expires": 4102444800000, "metadata": map[string]any{"agyHome": "bbbbbbbb"}},
	})
	plugin.UseCached([]plugin.Provider{{
		ID: "agy-cli", Spec: spec, Name: "Antigravity (local agy CLI)",
		Methods: []plugin.Method{{Type: "oauth", Label: "Sign in"}},
		Accounts: []plugin.Account{
			{Key: "agy-cli#a", Type: "oauth", AccountID: "a@example.com"},
			{Key: "agy-cli#b", Type: "oauth", AccountID: "b@example.com"},
			{Key: "agy-cli", Type: "oauth", AccountID: "own@example.com", Models: []string{
				"gemini-4-argon-high", "gemini-4-argon-medium", "gemini-4-argon-low",
				"claude-sonnet-5-5",
			}},
		},
		Models: listedModels(t, `[
			{"id":"gemini-4-argon","name":"Gemini 4 Argon"},
			{"id":"claude-sonnet-5-5","name":"Claude Sonnet 5.5"}
		]`),
	}})
	t.Cleanup(func() { plugin.UseCached(nil) })
	saveLogins(t,
		savedLogin{Agent: "plugin:agy-cli", User: "a@example.com", Home: "agy-cli#a", On: true, Order: 1},
		savedLogin{Agent: "plugin:agy-cli", User: "b@example.com", Home: "agy-cli#b", On: true, Order: 2},
		savedLogin{Agent: "plugin:agy-cli", User: "own@example.com", Home: "agy-cli", On: true, Order: 3},
	)
	pp := pluginAccounts()[0]
	out, left, _ := pp.AccountsFor("gemini-4-argon", nil)
	if !slices.ContainsFunc(out, func(a AccountOn) bool { return a.Account.pluginKey == "agy-cli" }) {
		t.Fatalf("the own account's raw argon variants did not match the family: out=%+v left=%+v", out, left)
	}
	if slices.ContainsFunc(left, func(a AccountOn) bool { return a.Account.pluginKey == "agy-cli" }) {
		t.Fatalf("the own account was left out: %+v", left)
	}
}
