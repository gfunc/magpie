package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/tidwall/jsonc"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/provider"
)

// The VS Code Server row: a User folder of its own under ~/.vscode-server,
// no command of its own (the server half of a Remote-SSH window installs no
// CLI), so a code on PATH — the desktop's — never says the server is here;
// its User folder does, and ~/.vscode-server a plain file says nothing can
// be written there
func TestVSCodeServerRow(t *testing.T) {
	home := syncHome(t)
	a, err := Find("vscode-server")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".vscode-server", "data", "User")
	if a.Name != "VS Code Server" || a.Bin != "" || a.Dir != dir || a.Path != filepath.Join(dir, "settings.json") {
		t.Fatalf("row: %+v", a)
	}
	if a.Detected() {
		t.Fatal("detected with nothing there")
	}

	// a code on PATH is the desktop's: the desktop row finds it (the lookup
	// works here), the server row has no bin to look for
	name := "code"
	if runtime.GOOS == "windows" {
		name += ".cmd"
		t.Setenv("PATHEXT", ".CMD")
	}
	binDir := t.TempDir()
	bin := filepath.Join(binDir, name)
	writeFile(t, bin, "not a runnable editor\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	if a.Detected() {
		t.Fatal("detected from a code on PATH alone")
	}
	if !vscodeAt(filepath.Join(home, "vscode", "User")).Detected() {
		t.Fatal("the desktop row did not find the code on PATH")
	}

	// its User folder says it
	writeFile(t, a.Path, "{}\n")
	if !a.Detected() {
		t.Fatal("not detected with its User folder")
	}

	// ~/.vscode-server a plain file: nothing can be written under it
	os.RemoveAll(filepath.Join(home, ".vscode-server"))
	writeFile(t, filepath.Join(home, ".vscode-server"), "taken")
	if a.Detected() {
		t.Fatal("detected with ~/.vscode-server a file")
	}
}

// A chatLanguageModels.json the user wrote magpie's group into by hand is
// adopted, not duplicated: synced, the one group is magpie's own — the
// catalog's models at the gateway's address — the user's other groups and
// comments kept; disconnected, the file is as it was
func TestVSCodeServerAdoptsHandWrittenGroup(t *testing.T) {
	syncHome(t)
	a, err := Find("vscode-server")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, a.Path, `{
	// my server editor
	"chat.defaultModel": "gpt-5",
	"remote.autoForwardPorts": true,
}
`)
	lm := filepath.Join(a.Dir, "chatLanguageModels.json")
	writeFile(t, lm, `// models I added
[
	{
		"name": "Ollama",
		"vendor": "ollama", // local
		"url": "http://localhost:11434"
	},
	{
		"name": "magpie",
		"vendor": "customendpoint",
		"apiType": "chat-completions",
		"models": [
			{
				"id": "group/auto-glm-5-3",
				"name": "GLM",
				"url": "http://127.0.0.1:3425/v1/chat/completions",
				"requestHeaders": {"Authorization": "Bearer magpie"}
			}
		]
	},
]
`)
	f := a.Field("model")
	// the hand-written group already counts as joined, so the model the
	// user is on stays their own
	if f.Get() != "gpt-5" || !a.Detected() || !a.Wired() {
		t.Fatalf("before: %q wired %v", f.Get(), a.Wired())
	}
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "gpt-5" {
		t.Fatalf("connect moved the user's model: %q", f.Get())
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	magpieGroups := func() []map[string]any {
		var list []map[string]any
		if err := json.Unmarshal(jsonc.ToJSON([]byte(readFile(lm))), &list); err != nil {
			t.Fatalf("groups: %v\n%s", err, readFile(lm))
		}
		var out []map[string]any
		for _, g := range list {
			if g["name"] == "magpie" && g["vendor"] == "customendpoint" {
				out = append(out, g)
			}
		}
		return out
	}
	if got := magpieGroups(); len(got) != 1 {
		t.Fatalf("magpie groups after sync: %d\n%s", len(got), readFile(lm))
	}
	g, ok := edit.GetJSONItem(lm, vscodeGroup)
	if !ok {
		t.Fatalf("magpie's group gone:\n%s", readFile(lm))
	}
	for k, want := range map[string]string{
		"apiType":     "chat-completions",
		"models.0.id": "relay/glm-4.6", "models.0.url": gatewayV1() + "/chat/completions",
		"models.0.requestHeaders.Authorization": "Bearer magpie",
	} {
		if got := gjson.Get(g, k).String(); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if _, ok := edit.GetJSONItem(lm, map[string]string{"name": "Ollama", "vendor": "ollama"}); !ok {
		t.Fatalf("user's group lost:\n%s", readFile(lm))
	}
	for _, keep := range []string{"// models I added", "// local", "// my server editor"} {
		if !strings.Contains(readFile(lm)+readFile(a.Path), keep) {
			t.Fatalf("lost %q", keep)
		}
	}
	if !a.Wired() || a.Check() != "" {
		t.Fatalf("wired %v, check %q", a.Wired(), a.Check())
	}

	// a model added to the catalog is listed once synced, the group still
	// one
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"glm-4.6", "other"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if g, _ := edit.GetJSONItem(lm, vscodeGroup); gjson.Get(g, "models.#").Int() != 2 {
		t.Fatalf("not synced: %s", g)
	}
	if got := magpieGroups(); len(got) != 1 {
		t.Fatalf("magpie groups after adding a model: %d\n%s", len(got), readFile(lm))
	}

	// the gateway's URL changed under it: Check says so
	b := readFile(lm)
	writeFile(t, lm, strings.ReplaceAll(b, gatewayV1(), "http://127.0.0.1:9/v1"))
	if c := a.Check(); !strings.Contains(c, "chatLanguageModels.json") {
		t.Fatalf("check: %q", c)
	}
	writeFile(t, lm, b)

	// disconnected: the user's group and their default model stay,
	// magpie's group goes
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if v, _ := edit.GetJSON(a.Path, vscodeDefault); v != "gpt-5" || a.Wired() {
		t.Fatalf("disconnected: %q %v", v, a.Wired())
	}
	if _, ok := edit.GetJSONItem(lm, vscodeGroup); ok {
		t.Fatalf("group left:\n%s", readFile(lm))
	}
	if _, ok := edit.GetJSONItem(lm, map[string]string{"name": "Ollama", "vendor": "ollama"}); !ok {
		t.Fatalf("user's group lost on disconnect:\n%s", readFile(lm))
	}
}

// installServerGen installs a VS Code Server generation named dirName under
// servers, of VS Code version productVersion, its bundled Copilot Chat of
// chatVersion declaring providers as its languageModelChatProviders.
func installServerGen(t *testing.T, servers, dirName, productVersion, chatVersion, providers string) {
	t.Helper()
	gen := filepath.Join(servers, dirName)
	writeFile(t, filepath.Join(gen, "server", "product.json"), `{"version":"`+productVersion+`","quality":"stable"}`)
	writeFile(t, filepath.Join(gen, "server", "extensions", "copilot", "package.json"), `{"name":"copilot-chat","publisher":"GitHub","version":"`+chatVersion+`","contributes":{"languageModelChatProviders":`+providers+`}}`)
}

// A server generation below the Custom Endpoint floor in use — its bundled
// chat gates customendpoint to non-stable builds — is said unsupported once
// wired; one at the floor gets the joined notice. The generation in use is
// lru.json's head, *.staging skipped
func TestVSCodeServerUnsupportedNotice(t *testing.T) {
	stable1 := "Stable-0123456789abcdef0123456789abcdef01234567"
	stable2 := "Stable-89abcdef012345670123456789abcdef01234567"
	gated := `[{"vendor":"copilot","displayName":"Copilot"},{"vendor":"customendpoint","when":"productQualityType != 'stable'","displayName":"Custom Endpoint"}]`
	ungated := `[{"vendor":"copilot","displayName":"Copilot"},{"vendor":"customendpoint","displayName":"Custom Endpoint"}]`

	setup := func(t *testing.T, lru []string) (*Agent, string) {
		home := syncHome(t)
		a, err := Find("vscode-server")
		if err != nil {
			t.Fatal(err)
		}
		servers := filepath.Join(home, ".vscode-server", "cli", "servers")
		if lru != nil {
			b, _ := json.Marshal(lru)
			writeFile(t, filepath.Join(servers, "lru.json"), string(b))
		}
		writeFile(t, a.Path, "{}\n")
		return a, servers
	}

	t.Run("gated", func(t *testing.T) {
		a, servers := setup(t, []string{stable1})
		installServerGen(t, servers, stable1, "1.121.0", "0.49.0", gated)
		if err := a.Connect(); err != nil {
			t.Fatal(err)
		}
		if n := a.Notice(); !strings.Contains(n, "neither a Custom Endpoint") || !strings.Contains(n, "0.49.0") {
			t.Fatalf("notice: %q", n)
		}
		if c := a.Check(); !strings.Contains(c, "neither a Custom Endpoint") {
			t.Fatalf("check: %q", c)
		}
	})

	t.Run("ungated", func(t *testing.T) {
		a, servers := setup(t, []string{stable1})
		installServerGen(t, servers, stable1, "1.122.0", "0.50.0", ungated)
		if err := a.Connect(); err != nil {
			t.Fatal(err)
		}
		if n := a.Notice(); !strings.Contains(n, "Chat model picker") {
			t.Fatalf("notice: %q", n)
		}
		if c := a.Check(); c != "" {
			t.Fatalf("check: %q", c)
		}
	})

	t.Run("staging skipped", func(t *testing.T) {
		a, servers := setup(t, []string{stable2 + ".staging", stable1})
		installServerGen(t, servers, stable2+".staging", "1.142.0", "0.70.0", ungated)
		installServerGen(t, servers, stable1, "1.121.0", "0.49.0", gated)
		if err := a.Connect(); err != nil {
			t.Fatal(err)
		}
		if n := a.Notice(); !strings.Contains(n, "neither a Custom Endpoint") {
			t.Fatalf("notice: %q", n)
		}
	})
}

// The Custom Endpoint floor read from the server generation in use: no
// server is nothing to check; an unreadable lru.json falls back to the
// newest generation by its product.json's version; a chat without the
// provider at all is unsupported; a chat whose package.json can't be read
// falls back to the generation's own version against the floor; a
// generation nothing can be read of is unknown, not unsupported
func TestVSCodeServerChatFloor(t *testing.T) {
	stable1 := "Stable-0123456789abcdef0123456789abcdef01234567"
	stable2 := "Stable-89abcdef012345670123456789abcdef01234567"
	gated := `[{"vendor":"copilot"},{"vendor":"customendpoint","when":"productQualityType != 'stable'"}]`
	ungated := `[{"vendor":"copilot"},{"vendor":"customendpoint"}]`

	// no server installed: the Custom Endpoint group stays as always
	if c := vscodeServerChat(filepath.Join(t.TempDir(), "missing")); c.vendor != vscodeEndpoint || c.none || c.unsupported {
		t.Fatalf("missing: %+v", c)
	}

	// an unreadable lru.json: the newest generation by its product.json's
	// version answers
	servers := filepath.Join(t.TempDir(), "servers")
	installServerGen(t, servers, stable1, "1.121.0", "0.49.0", gated)
	installServerGen(t, servers, stable2, "1.142.0", "0.61.0", ungated)
	writeFile(t, filepath.Join(servers, "lru.json"), "{not json")
	if c := vscodeServerChat(servers); c.vendor != vscodeEndpoint || c.unsupported || c.version != "0.61.0" {
		t.Fatalf("scan: %+v", c)
	}

	// the provider left out entirely: unsupported
	servers = filepath.Join(t.TempDir(), "servers")
	installServerGen(t, servers, stable1, "1.121.0", "0.48.1", `[{"vendor":"copilot"}]`)
	writeFile(t, filepath.Join(servers, "lru.json"), `["`+stable1+`"]`)
	if c := vscodeServerChat(servers); !c.unsupported || c.version != "0.48.1" {
		t.Fatalf("absent: %+v", c)
	}

	// the chat's package.json unreadable: the generation's own version
	// against the floor
	servers = filepath.Join(t.TempDir(), "servers")
	writeFile(t, filepath.Join(servers, stable1, "server", "extensions", "copilot", "package.json"), `{not json`)
	product := filepath.Join(servers, stable1, "server", "product.json")
	writeFile(t, product, `{"version":"1.121.0"}`)
	writeFile(t, filepath.Join(servers, "lru.json"), `["`+stable1+`"]`)
	if c := vscodeServerChat(servers); !c.unsupported || c.version != "1.121.0" {
		t.Fatalf("fallback below floor: %+v", c)
	}
	writeFile(t, product, `{"version":"1.122.0"}`)
	if c := vscodeServerChat(servers); c.unsupported || c.version != "1.122.0" {
		t.Fatalf("fallback at floor: %+v", c)
	}
	writeFile(t, product, `{"version":"1.142.0"}`)
	if c := vscodeServerChat(servers); c.unsupported || c.version != "1.142.0" {
		t.Fatalf("fallback above floor: %+v", c)
	}

	// nothing of the generation readable: unknown, not unsupported
	servers = filepath.Join(t.TempDir(), "servers")
	if err := os.MkdirAll(filepath.Join(servers, stable1, "server"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(servers, "lru.json"), `["`+stable1+`"]`)
	if c := vscodeServerChat(servers); c.vendor != vscodeEndpoint || c.none || c.unsupported {
		t.Fatalf("unknown: %+v", c)
	}
}
