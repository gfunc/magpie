package provider

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
)

// Native is the API a client does best to ask a model on, for the gateway
// to relay it as it is.
func TestNative(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	// Copilot's model list says which APIs serve each model
	if err := catalog.SaveLive("copilot", "https://api.githubcopilot.com", []catalog.Model{
		{ID: "gpt-5.5", APIs: []string{"responses"}}, {ID: "claude-sonnet-5", APIs: []string{"chat", "anthropic"}}}); err != nil {
		t.Fatal(err)
	}
	openai := Provider{ID: "openai", Chat: "https://api.openai.com/v1", Responses: "https://api.openai.com/v1"}
	copilot := Provider{ID: "copilot", Chat: "https://api.githubcopilot.com", Responses: "https://api.githubcopilot.com", Anthropic: "https://api.githubcopilot.com"}
	for _, c := range []struct {
		name  string
		p     Provider
		model string
		want  Protocol
	}{
		{"chat only", Provider{ID: "relay", Chat: "https://relay.test/v1"}, "glm-4.6", Chat},
		{"responses only", Provider{ID: "resp", Responses: "https://resp.test/v1"}, "m", Responses},
		{"ChatGPT sign-in", Provider{ID: "codex", Responses: CodexBase, Account: &Account{Agent: "codex"}}, "gpt-5.5", Responses},
		{"GPT on OpenAI's API", openai, "gpt-5.5", Responses},
		{"o-series on OpenAI's API", openai, "o3", Responses},
		{"other on OpenAI's API", openai, "whisper-x", Chat},
		{"Chat and Responses elsewhere", Provider{ID: "both", Chat: "https://x.test/v1", Responses: "https://x.test/v1"}, "gpt-5.5", Chat},
		// the API the user gave a custom provider's Base URL as, where a
		// Chat URL was found beside it (nb5p on Discord)
		{"Responses picked beside Chat", Provider{ID: "mygateway", Chat: "https://x.test/v1", Responses: "https://x.test/v1", BaseAPI: "responses"}, "gpt-6.1-sol", Responses},
		{"Chat picked beside Responses", Provider{ID: "mygateway", Chat: "https://x.test/v1", Responses: "https://x.test/v1", BaseAPI: "chat"}, "gpt-6.1-sol", Chat},
		{"Responses picked, Claude on Messages", Provider{ID: "mygateway", Responses: "https://x.test/v1", Anthropic: "https://x.test", BaseAPI: "responses"}, "claude-sonnet-5", Anthropic},
		{"Responses picked, no Responses URL", Provider{ID: "mygateway", Chat: "https://x.test/v1", BaseAPI: "responses"}, "m", Chat},
		{"anthropic only", Provider{ID: "anthropic", Anthropic: "https://api.anthropic.com"}, "claude-sonnet-5", Anthropic},
		{"Copilot GPT", copilot, "gpt-5.5", Responses},
		{"Copilot Claude", copilot, "claude-sonnet-5", Anthropic},
		{"Claude Code", Provider{ID: "claude", Anthropic: "https://api.anthropic.com", Account: &Account{Agent: "claude"}}, "claude-sonnet-5", ""},
		{"nothing", Provider{ID: "none"}, "m", ""},
	} {
		if got := c.p.Native(c.model); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// A model the provider serves on Gemini's generateContent as it is is
// listed on that wire too (/v1/models' native_endpoints), which relayed
// alone leaves out: its Native gate picks among the three wires magpie
// translates between, and a generateContent-only model has no native one
// of those. The Gemini path is keyed by the id the client sends
// (Entry.ID), the one the gateway's Gemini route takes in its path.
func TestNativeEndpointsOnGemini(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	// Vertex AI says, per model, which API serves it: its Gemini models
	// on generateContent alone, partner models on chat completions and
	// Claude's on Anthropic's Messages (ListedAPIs)
	vertex := Provider{ID: "vertex-prod", Name: "Vertex prod", Preset: VertexPreset, Vertex: &Vertex{Project: "acme-vertex"}}
	// a relay with a Gemini API beside its Chat one, nothing said per model
	both := Provider{ID: "both", Chat: "https://both.test/v1", Gemini: "https://both.test/v1beta"}
	// a Google sign-in speaks Code Assist, never Gemini, and a Claude
	// Code or Devin sign-in translates generateContent like every other
	// request, so none of them is listed on the wire — even one whose
	// saved shape carries a Gemini base
	codeAssist := Provider{ID: "gemini", Account: &Account{Agent: "gemini", codeAssist: "https://cloudcode-pa.googleapis.com"}}
	claude := Provider{ID: "claude", Anthropic: "https://api.anthropic.com", Gemini: "https://generativelanguage.googleapis.com/v1beta", Account: &Account{Agent: "claude"}}
	devin := Provider{ID: "devin", Gemini: "https://x.test", Account: &Account{Agent: "devin"}}
	for _, c := range []struct {
		name string
		e    Entry
		want []string
	}{
		// the case relayed alone returns nil for: Native("") gates a
		// generateContent-only model out of the translated three
		{"Vertex's Gemini model, keyed by the client's id", Entry{ID: "vertex-prod/gemini-3-pro", Model: "gemini-3-pro", Provider: &vertex}, []string{"/v1beta/models/vertex-prod/gemini-3-pro:generateContent"}},
		{"Vertex's partner model stays on Chat", Entry{ID: "vertex-prod/deepseek-ai/deepseek-v4.1-flash-maas", Model: "deepseek-ai/deepseek-v4.1-flash-maas", Provider: &vertex}, []string{"/v1/chat/completions"}},
		{"Vertex's Claude model stays on Messages", Entry{ID: "vertex-prod/claude-opus-5-5", Model: "claude-opus-5-5", Provider: &vertex}, []string{"/v1/messages"}},
		{"a Gemini-only base", Entry{ID: "mine/gemini-3-pro", Model: "gemini-3-pro", Provider: &Provider{ID: "mine", Gemini: "https://generativelanguage.googleapis.com/v1beta"}}, []string{"/v1beta/models/mine/gemini-3-pro:generateContent"}},
		{"each wire it speaks, nothing said per model", Entry{ID: "both/glm-5.3", Model: "glm-5.3", Provider: &both}, []string{"/v1/chat/completions", "/v1beta/models/both/glm-5.3:generateContent"}},
		{"Claude Code", Entry{ID: "claude/claude-sonnet-5", Model: "claude-sonnet-5", Provider: &claude}, nil},
		{"Devin", Entry{ID: "devin/swe-2", Model: "swe-2", Provider: &devin}, nil},
		{"Code Assist", Entry{ID: "gemini/gemini-3-pro", Model: "gemini-3-pro", Provider: &codeAssist}, nil},
	} {
		if got := NativeEndpoints(c.e); !slices.Equal(got, c.want) {
			t.Errorf("%s: endpoints %v, want %v", c.name, got, c.want)
		}
	}
}

// A routing group rides the OpenAI-compatible wires: its endpoints are
// its members' common APIs among the three magpie translates between,
// never Gemini's per-model path — an all-Gemini group has none.
func TestNativeEndpointsOfAGroup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	geminiOnly := Provider{ID: "mine", Gemini: "https://generativelanguage.googleapis.com/v1beta"}
	both := Provider{ID: "both", Chat: "https://both.test/v1", Gemini: "https://both.test/v1beta"}
	chat := Provider{ID: "chat", Chat: "https://chat.test/v1"}
	of := func(ps ...Provider) []Member {
		var ms []Member
		for _, p := range ps {
			ms = append(ms, Member{Provider: p, Model: "glm-5.3"})
		}
		return ms
	}
	for _, c := range []struct {
		name string
		ms   []Member
		want []string
	}{
		{"an all-Gemini group has no path", of(geminiOnly, geminiOnly), nil},
		{"members sharing no translated wire", of(both, geminiOnly), nil},
		{"the Chat wire both speak, not the Gemini one a member also speaks", of(both, chat), []string{"/v1/chat/completions"}},
	} {
		if got := GroupEndpoints(c.ms); !slices.Equal(got, c.want) {
			t.Errorf("%s: endpoints %v, want %v", c.name, got, c.want)
		}
	}
}
