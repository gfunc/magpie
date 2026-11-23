package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// goldenStep is one agent's hooks file, and magpie's records of what it
// wrote there, after one step of TestHooksGoldenBytes's script.
type goldenStep struct {
	Name    string      `json:"name"`
	File    string      `json:"file"`
	Applied []HookEntry `json:"applied"`
	// Row is dsh's profile row, the one other file a hooks sync writes
	Row string `json:"row,omitempty"`
}

type goldenFile struct {
	Steps []goldenStep `json:"steps"`
}

// goldenHome stands in for the sandbox's home in a golden. The folder is a
// different one on every run, and dsh's row names the hooks file in it.
const goldenHome = "<home>"

// TestHooksGoldenBytes holds what the hooks engine writes today, byte for
// byte, for the seven agents it serves, and the records it keeps of that in
// library.json. The engine is being rewritten to serve one more agent. This
// test must pass against the same goldens afterwards, so a change to any
// byte an existing agent gets is caught before it ships.
func TestHooksGoldenBytes(t *testing.T) {
	update := os.Getenv("UPDATE_HOOKS_GOLDEN") == "1"
	for _, id := range hookAgents {
		t.Run(id, func(t *testing.T) {
			got := goldenRun(t, id)
			path := filepath.Join("testdata", "hooks-golden", id+".json")
			if update {
				goldenWrite(t, path, got)
				return
			}
			goldenCompare(t, path, got)
		})
	}
}

// goldenSeed is the user's own hooks file before magpie writes into it:
// their settings, their comments, and hooks of their own.
var goldenSeed = map[string]string{
	"claude": `{
  // my own settings
  "model": "opus",
  "hooks": {
    // said when a turn ends
    "Stop": [{"hooks": [{"type": "command", "command": "say done"}]}],
    "PostToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "my-log"}]}]
  }
}
`,
	"codex": `{
  // the hooks I run in Codex
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "say done"}]}],
    "PostToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "my-log"}]}]
  }
}
`,
	"cursor": `{
  // my Cursor hooks
  "version": 1,
  "hooks": {
    "stop": [{"command": "say done"}],
    "postToolUse": [{"command": "my-log", "matcher": "Shell"}]
  }
}
`,
	"gemini": `{
  // my Gemini settings
  "theme": "Default",
  "hooks": {
    "AfterAgent": [{"hooks": [{"type": "command", "command": "say done"}]}],
    "AfterTool": [{"matcher": "run_shell_command", "hooks": [{"type": "command", "command": "my-log"}]}]
  }
}
`,
	"grok": `# my Grok Build config
model = "grok-4.7"

# my own hooks
[[hooks.Stop]]
  [[hooks.Stop.hooks]]
  type = "command"
  command = "say done"

[[hooks.Stop]]
  [[hooks.Stop.hooks]]
  type = "command"
  command = "say again"

[[hooks.PostToolUse]]
matcher = "run_terminal_command"
  [[hooks.PostToolUse.hooks]]
  type = "command"
  command = "my-log"
`,
	"grok-app": `[models]
default = "grok-4.7"

# the Grok App's own hooks
[[hooks.Stop]]
  [[hooks.Stop.hooks]]
  type = "command"
  command = "say done"

[[hooks.PostToolUse]]
matcher = "run_terminal_command"
  [[hooks.PostToolUse.hooks]]
  type = "command"
  command = "my-log"
`,
	"dsh": `{
  // the hooks dsh runs through its plugin
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "say done"}]}]
  }
}
`,
}

// goldenRun plays the script for agent id in a sandbox of its own, and
// snapshots its files and records after every step.
func goldenRun(t *testing.T, id string) goldenFile {
	t.Helper()
	h := sandbox(t)
	// each agent's hooks file, and where its seed goes
	var file, row string
	switch id {
	case "claude":
		file = filepath.Join(h, ".claude", "settings.json")
	case "codex":
		file = filepath.Join(h, ".codex", "hooks.json")
	case "cursor":
		file = filepath.Join(h, ".cursor", "hooks.json")
	case "gemini":
		file = filepath.Join(h, ".gemini", "settings.json")
	case "grok":
		file = filepath.Join(h, ".grok", "config.toml")
	case "grok-app":
		file = filepath.Join(h, "grok-app-data", "agent-home", "config.toml")
	case "dsh":
		file = filepath.Join(h, ".dsh", "magpie-hooks.json")
		row = filepath.Join(h, ".dsh", "profiles", "web", "cordis.patch.yml")
	default:
		// an agent the engine serves that this script doesn't know: its
		// file would never be read, and an empty golden would pass
		t.Fatalf("%s is served hooks but has no file and seed here: add them, and a golden", id)
	}
	if id == "grok" || id == "grok-app" {
		// the App reads agent-home/config.toml under its data folder
		t.Setenv("GROK_APP_HOME", filepath.Join(h, "grok-app-data"))
		write(t, filepath.Join(h, "grok-app-data", "agent-home", "config.toml"), goldenSeed["grok-app"])
	}
	if id == "dsh" {
		// dsh runs its hooks file through a plugin the sync installs first
		fakeDshPlugins(t, "0.2.2-rc.5", nil)
	}
	write(t, file, goldenSeed[id])

	g := goldenFile{Steps: []goldenStep{}}
	snap := func(name string) {
		t.Helper()
		s := goldenStep{Name: name, File: goldenText(read(t, file)), Applied: []HookEntry{}}
		if a := mustLoad(t).Applied[id]; a != nil {
			s.Applied = a.Hooks
		}
		if row != "" {
			s.Row = goldenText(read(t, row))
		}
		g.Steps = append(g.Steps, s)
	}
	found := func(cmd string) string {
		t.Helper()
		v, err := Read(nil)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(v.FoundHooks, func(f FoundHook) bool { return f.Agent == id && slices.Equal(f.Commands, []string{cmd}) })
		if i < 0 {
			t.Fatalf("%s: no found hook running %q: %+v", id, cmd, v.FoundHooks)
		}
		return v.FoundHooks[i].Key
	}

	snap("seed")
	ok(t)(SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "curl -s https://api.day.app/KEY/done", Timeout: 10, Agents: []string{id}}))
	snap("save turn-end")
	if _, err := Read(nil); err != nil {
		t.Fatal(err)
	}
	// a sync with nothing to do goes the whole way through, and writes nothing
	ok(t)(HookAgents("bark", []string{id}))
	snap("sync no change")
	ok(t)(SaveHook("", Hook{Name: "fmt", Event: "after-tool", Matcher: "edit", Command: "prettier --write .", Agents: []string{id}}))
	snap("save after-tool")
	ok(t)(SaveHook("bark", Hook{Name: "bark", Event: "turn-end", Command: "curl -s https://api.day.app/KEY/again", Timeout: 10, Agents: []string{id}}))
	snap("edit command")
	// the user adds one of their own after magpie's
	write(t, file, goldenAfterMine(t, id, read(t, file), "curl -s https://api.day.app/KEY/again"))
	ok(t)(HookAgents("bark", []string{}))
	snap("untick first")
	// the user edits magpie's entry by hand
	write(t, file, strings.Replace(read(t, file), "prettier --write .", "prettier --write ./src", 1))
	ok(t)(HookAgents("fmt", []string{}))
	snap("untick user-edited")
	if id != "dsh" {
		// dsh's file is magpie's own: it has no hook of the user's to find
		ok(t)(ImportHooks([]string{found("my-log")}))
		snap("import found")
		ok(t)(RemoveFoundHooks([]string{found("say done")}))
		snap("remove found")
	}
	return g
}

// goldenAfter is one more of the user's own turn-end hooks, as each
// agent's file holds it. A JSON agent's is the entry alone, with the
// indent magpie gives a list it writes into.
var goldenAfter = map[string]string{
	"claude": `      {
        "hooks": [
          {
            "type": "command",
            "command": "say after"
          }
        ]
      }`,
	"codex": `      {
        "hooks": [
          {
            "type": "command",
            "command": "say after"
          }
        ]
      }`,
	"cursor": `      {
        "command": "say after"
      }`,
	"gemini": `      {
        "hooks": [
          {
            "type": "command",
            "command": "say after"
          }
        ]
      }`,
	"dsh": `      {
        "hooks": [
          {
            "type": "command",
            "command": "say after"
          }
        ]
      }`,
	"grok": `
[[hooks.Stop]]
  [[hooks.Stop.hooks]]
  type = "command"
  command = "say after"
`,
	"grok-app": `
[[hooks.Stop]]
  [[hooks.Stop.hooks]]
  type = "command"
  command = "say after"
`,
}

// goldenAfterMine is the hooks file with the user's turn-end hook after
// magpie's, so magpie's sits between two of theirs. Text only: a user's
// edit does not re-space what is around it. A JSON agent's list takes the
// entry before its closing bracket, a Grok file the block at the end.
func goldenAfterMine(t *testing.T, id, s, anchor string) string {
	t.Helper()
	entry, ok := goldenAfter[id]
	if !ok {
		t.Fatalf("no entry of the user's own to add for %s", id)
	}
	if id == "grok" || id == "grok-app" {
		return strings.TrimSuffix(s, "\n") + "\n" + entry
	}
	i := strings.Index(s, anchor)
	if i < 0 {
		t.Fatalf("magpie's entry %q isn't in the file to add one after:\n%s", anchor, s)
	}
	const close = "\n    ]"
	j := strings.Index(s[i:], close)
	if j < 0 {
		t.Fatalf("no list closes after %q:\n%s", anchor, s)
	}
	at := i + j
	return s[:at] + ",\n" + entry + close + s[at+len(close):]
}

// goldenText is a file's text as a golden holds it.
func goldenText(s string) string {
	return strings.ReplaceAll(s, os.Getenv("HOME"), goldenHome)
}

// goldenRender is a run's snapshots as the golden file's text.
func goldenRender(t *testing.T, g goldenFile) string {
	t.Helper()
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func goldenWrite(t *testing.T, path string, g goldenFile) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(goldenRender(t, g)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// goldenCompare fails unless the golden file's text is exactly what this run
// would write. The golden is never parsed: reading it back into a type
// would compact what is inside an entry, and a byte moved there would not
// be a difference. The per-step messages say where to look.
func goldenCompare(t *testing.T, path string, got goldenFile) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no golden at %s: %v (UPDATE_HOOKS_GOLDEN=1 writes it)", path, err)
	}
	want := string(b)
	if text := goldenRender(t, got); text != want {
		t.Errorf("%s: not what the golden holds\n%s", path, goldenDiff(want, text))
	}
	// as far as the golden parses, each step and field says what differs
	var w goldenFile
	if err := json.Unmarshal(b, &w); err != nil {
		return
	}
	if len(w.Steps) != len(got.Steps) {
		t.Errorf("the golden has %d steps, this run took %d", len(w.Steps), len(got.Steps))
		return
	}
	for i, ws := range w.Steps {
		g := got.Steps[i]
		if ws.Name != g.Name {
			t.Errorf("step %d: the golden calls it %q, this run %q", i, ws.Name, g.Name)
			continue
		}
		for _, c := range []struct{ what, want, got string }{
			{"file", ws.File, g.File},
			{"row", ws.Row, g.Row},
			{"applied", goldenJSON(ws.Applied), goldenJSON(g.Applied)},
		} {
			if c.want != c.got {
				t.Errorf("step %q: %s differs from the golden\n%s", ws.Name, c.what, goldenDiff(c.want, c.got))
			}
		}
	}
}

// goldenJSON is a value as the golden holds it, compared as text: the
// records are never read back into a type and compared field by field, so a
// byte that moved inside an entry is a difference too.
func goldenJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// goldenDiff is the golden's text and this run's, from the first line that
// differs, a few lines either side.
func goldenDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	i := 0
	for i < len(w) && i < len(g) && w[i] == g[i] {
		i++
	}
	from, to := max(i-3, 0), min(i+3, len(w), len(g))
	var b strings.Builder
	for j := from; j < i; j++ {
		fmt.Fprintf(&b, "  %s\n", w[j])
	}
	if i < len(w) {
		fmt.Fprintf(&b, "- %s\n", w[i])
	}
	if i < len(g) {
		fmt.Fprintf(&b, "+ %s\n", g[i])
	}
	for j := i + 1; j < to; j++ {
		fmt.Fprintf(&b, "  %s\n", w[j])
	}
	if len(w) != len(g) {
		fmt.Fprintf(&b, "(the golden has %d lines, this run %d)\n", len(w), len(g))
	}
	return b.String()
}
