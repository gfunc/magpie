package agent

// VS Code Server is the server half of a Remote-SSH window: a VS Code that
// runs headless under ~/.vscode-server on the machine the window is into,
// with a User folder of its own there (data/User) and a Copilot Chat
// bundled in each installed generation, so magpie's group is always written
// for the Custom Endpoint provider — when the generation in use has it.
// Before VS Code 1.122 (bundled chat 0.50.0) customendpoint is gated behind
// productQualityType != 'stable', which a stable server build never
// satisfies; a generation like that gets the unsupported notice
// (noticeVSCodeOld). The generation in use is read from cli/servers/lru.json
// (its head first, *.staging skipped), or without a readable lru.json the
// newest generation by its product.json's version. A generation whose
// bundled chat's package.json can't be read falls back to that product.json
// version against the floor; one nothing can be read of is unknown, not
// unsupported.
//
// Its chat names itself to a Custom Endpoint as desktop Stable's does
// (GitHubCopilotChat/<version>), so its models send the gateway's own token
// and its requests are the desktop row's by their User-Agent.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
)

// vscodeServerKind has no bin and no extensions folder of its own: detected
// by its User folder alone, with server set instead.
var vscodeServerKind = vscodeKind{id: "vscode-server", name: "VS Code Server", ua: []string{vscodeUA}}

func vscodeServer(home string) *Agent {
	k := vscodeServerKind
	k.server = filepath.Join(home, ".vscode-server", "cli", "servers")
	return vscodeKindAt(k, filepath.Join(home, ".vscode-server", "data", "User"))
}

// vscodeServerChat is the bundled Copilot Chat of the VS Code Server
// generation in use under servers: the first generation with a definitive
// answer. With none — no server installed, or none readable — the Custom
// Endpoint group stays as magpie always wrote it.
func vscodeServerChat(servers string) vscodeChat {
	for _, gen := range vscodeServerGens(servers) {
		if c, ok := vscodeServerChatOf(gen); ok {
			return c
		}
	}
	return vscodeChat{vendor: vscodeEndpoint}
}

// vscodeServerGens are the installed generations under servers, the one
// most likely in use first: lru.json's order with *.staging skipped, or
// without a readable lru.json every non-staging generation, newest first by
// its product.json's version (an unreadable one last).
func vscodeServerGens(servers string) []string {
	var gens []string
	if b, err := os.ReadFile(filepath.Join(servers, "lru.json")); err == nil {
		var lru []string
		if json.Unmarshal(b, &lru) == nil {
			for _, name := range lru {
				if strings.HasSuffix(name, ".staging") {
					continue
				}
				gen := filepath.Join(servers, name)
				if isDir(gen) {
					gens = append(gens, gen)
				}
			}
		}
	}
	if len(gens) > 0 {
		return gens
	}
	ents, err := os.ReadDir(servers)
	if err != nil {
		return nil
	}
	type gen struct{ dir, version string }
	var all []gen
	for _, e := range ents {
		if !e.IsDir() || strings.HasSuffix(e.Name(), ".staging") {
			continue
		}
		dir := filepath.Join(servers, e.Name())
		all = append(all, gen{dir, vscodeServerVersion(dir)})
	}
	sort.SliceStable(all, func(i, j int) bool {
		return vscodeNewer(all[i].version, all[j].version)
	})
	for _, g := range all {
		gens = append(gens, g.dir)
	}
	return gens
}

// vscodeServerChatOf is the bundled Copilot Chat of one installed
// generation: the Custom Endpoint provider, unsupported when the chat's
// declared providers gate it to non-stable builds (or leave it out), with
// the chat's version. ok is false when nothing of the generation can be
// read: unknown is not unsupported.
func vscodeServerChatOf(gen string) (vscodeChat, bool) {
	b, err := os.ReadFile(filepath.Join(gen, "server", "extensions", "copilot", "package.json"))
	if err == nil && gjson.ValidBytes(b) {
		version := gjson.GetBytes(b, "version").String()
		supported := false
		for _, p := range gjson.GetBytes(b, "contributes.languageModelChatProviders").Array() {
			if p.Get("vendor").String() == vscodeEndpoint && !vscodeInsidersGate(p.Get("when").String()) {
				supported = true
			}
		}
		return vscodeChat{vendor: vscodeEndpoint, version: version, unsupported: !supported}, true
	}
	// an unreadable chat package.json: the generation's own VS Code version
	// against the floor
	if v := vscodeServerVersion(gen); v != "" {
		return vscodeChat{vendor: vscodeEndpoint, version: v, unsupported: vscodeNewer("1.122", v)}, true
	}
	return vscodeChat{}, false
}

// vscodeInsidersGate is whether a chat provider's when clause keeps it to
// non-stable builds: customendpoint's "productQualityType != 'stable'"
// before VS Code 1.122.
func vscodeInsidersGate(when string) bool {
	return strings.Contains(strings.NewReplacer(" ", "", "\t", "").Replace(when), "productQualityType!='stable'")
}

// vscodeServerVersion is the VS Code version of an installed generation,
// from its product.json; "" when it can't be read.
func vscodeServerVersion(gen string) string {
	b, err := os.ReadFile(filepath.Join(gen, "server", "product.json"))
	if err != nil || !gjson.ValidBytes(b) {
		return ""
	}
	return gjson.GetBytes(b, "version").String()
}
