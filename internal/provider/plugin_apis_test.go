package provider

import (
	"slices"
	"testing"

	"github.com/yetone/magpie/internal/plugin"
)

// A model a chat-completions plugin does not list is spoken to on chat
// completions, and only that (#8025): with "any API" a Messages client's
// request went to the plugin as Messages, which the plugin relayed to its
// own Messages endpoint - one that serves its Claude models only, answering
// 400 for a chat model there (WorkBuddy, on the China build).
func TestPluginAPIsUnlistedChatModelIsChatOnly(t *testing.T) {
	p := Provider{Account: &Account{}}
	p.Account.plugin = &plugin.Provider{ID: "probe-plugin", NPM: "@ai-sdk/openai-compatible"}
	if got := p.pluginAPIs("a-model-the-plugin-does-not-list"); !slices.Equal(got, []Protocol{Chat}) {
		t.Fatalf("pluginAPIs(unlisted) = %v, want [chat]: with any API a Messages client is relayed as Messages", protocolNames(got))
	}
}

// A plugin that reads only Anthropic Messages is spoken to on Messages for a
// model it does not list, base or no base (#7908): relaying a chat client's
// body to it is what it refuses - "image_url is not supported", "a tool
// without a valid name".
func TestPluginAPIsUnlistedModelOfAnAnthropicPluginIsMessages(t *testing.T) {
	p := Provider{Account: &Account{}}
	p.Account.plugin = &plugin.Provider{ID: "probe-plugin", NPM: "@ai-sdk/anthropic"}
	if got := p.pluginAPIs("a-model-the-plugin-does-not-list"); !slices.Equal(got, []Protocol{Anthropic}) {
		t.Fatalf("pluginAPIs(unlisted) = %v, want [anthropic]: a chat body there is refused", protocolNames(got))
	}
}
