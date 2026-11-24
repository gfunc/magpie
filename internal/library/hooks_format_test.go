package library

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Claude Code plugin's hook is given the variables Claude Code gives its
// own (pluginHookEnv) before the format spells its entry. The shell each OS
// picks is TestPluginHookEnvPerShell's to pin.
func TestHookFormatClaudePluginBundle(t *testing.T) {
	sp := claudeHookSpec(filepath.Join(t.TempDir(), "settings.json"))
	raw, err := sp.entry(&Hook{Name: "x", Bundle: "it's", Event: "turn-end", Command: "run"}, "Stop")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"command":"export CLAUDE_PLUGIN_ROOT=`) || !strings.Contains(string(raw), "CLAUDE_PLUGIN_DATA=") {
		t.Errorf("a plugin's hook doesn't set the plugin's variables: %s", raw)
	}
	// a hook of the user's own is as they wrote it
	raw, err = sp.entry(&Hook{Name: "x", Event: "turn-end", Command: "run"}, "Stop")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "CLAUDE_PLUGIN") || strings.Contains(string(raw), `"shell"`) {
		t.Errorf("the user's own hook was given the variables: %s", raw)
	}
}

// respacedFormat is a hooks file another tool re-serialises: an entry magpie
// wrote comes back as the same rule with other keys and spacing. Two are one
// hook when they say the same thing, whatever their text.
type respacedFormat struct{}

func (respacedFormat) Open(path string) (hookFile, error) {
	f, err := (jsonFormat{}).Open(path)
	if err != nil {
		return nil, err
	}
	return &respacedFile{f.(*jsonFile)}, nil
}

func (respacedFormat) Entry(in hookInput) (json.RawMessage, error) { return (jsonFormat{}).Entry(in) }

func (respacedFormat) Decode(ev string, e json.RawMessage) (decodedHook, error) {
	return (jsonFormat{}).Decode(ev, e)
}

// Same is the format's normal form: the command the entry runs.
func (respacedFormat) Same(a, b json.RawMessage) bool {
	var e struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	return json.Unmarshal(a, &e) == nil && len(e.Hooks) == 1 && string(b) != "" && strings.Contains(string(b), e.Hooks[0].Command)
}

// respacedFile counts the writes a sync makes.
type respacedFile struct{ *jsonFile }

var respacedWrites int

func (f *respacedFile) Write(path string, drop map[string][]int, add map[string][]json.RawMessage, lists map[string][]json.RawMessage) error {
	respacedWrites++
	return errors.New("a sync that had nothing to change reached the write")
}

// Every place the engine asks "are these one entry" asks the format. In
// run() there are two: find, which says which file entry a record or a
// wanted entry is, and the done test, which says whether a claimed record is
// the wanted entry. In hooks_found.go there is a third, the claim pass,
// which says which file entries magpie's records own. This pins find: left
// on exact text while the format compares in a normal form, a record whose
// file entry came back re-serialised is taken as gone, and its hook written
// a second time.
//
// Kimi Code pins the claim pass too (TestKimiHooksResavedBlockIsStillMine,
// hooks_kimi_sync_test.go): on exact text, a block of magpie's the user's
// tool saved again is listed under "In your agents" as the user's.
//
// The done test is on the format and is pinned by nothing. It compares an
// entry magpie just built with a record magpie wrote, and for every format
// in this tree those two are the same text, Entry being deterministic, so no
// state tells a done on exact text from one on the normal form: measured,
// a sync over a re-saved block with done on canonical writes nothing and
// keeps the record. It is left for a format whose records and fresh entries
// can differ in their spelling and still be one rule.
func TestHookFormatSameIsIdentityAtFindAndClaim(t *testing.T) {
	// One rule in two spellings: as the format spells it, and as the file
	// holds it now that the other tool re-saved it, with a key of its own
	// added. Their text differs, which canonical tells apart, and the
	// format's normal form does not.
	const mine = `{"hooks":[{"type":"command","command":"say done"}]}`
	const inFile = `{ "hooks": [ { "command": "say done", "type": "command", "_source": "theirs" } ] }`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"hooks":{"Stop":[`+inFile+`]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &Hook{Name: "bark", Event: "turn-end", Command: "say done"}
	sp := &hookSpec{path: path, events: map[string]string{"turn-end": "Stop"},
		native: []string{"Stop"}, tools: map[string]string{}, agent: "fake", format: respacedFormat{}}
	// magpie's record of what it wrote, which the file now holds re-spaced
	recs := []HookEntry{{Name: "bark", Event: "Stop", Entry: json.RawMessage(mine)}}
	var problems []error
	w := &hookWrite{id: "fake", sp: sp, want: []*Hook{h}, recs: recs, fail: func(_ string, err error) { problems = append(problems, err) }}
	respacedWrites = 0
	got, changed, ok := w.run()
	if len(problems) > 0 {
		t.Fatalf("the sync said: %v", problems)
	}
	if !ok {
		t.Fatal("the sync didn't finish")
	}
	if changed || respacedWrites != 0 {
		t.Errorf("the record's entry in the file wasn't recognised, so the hook was written again: changed=%v, writes=%d", changed, respacedWrites)
	}
	if len(got) != 1 || got[0].Name != "bark" || string(got[0].Entry) != mine {
		t.Errorf("records after the sync: %+v", got)
	}
}
