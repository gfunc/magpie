package library

import (
	"encoding/json"
	"errors"

	"github.com/pelletier/go-toml/v2"
	"github.com/tidwall/gjson"
)

// A hook is kept once (Hook) and written into each agent ticked for it, in
// that agent's own file. How that file is laid out is behind hookFormat:
// nothing outside a format asks which agent a hooks file is. The engine
// (hookWrite.run) and the discovery reader (hooks_found.go) work on an
// event name, an entry as json.RawMessage, a list, and a set of indices.

// hookInput is one hook on its way into an agent's file. Timeout is Library
// seconds: 0 is the agent's own.
type hookInput struct {
	Event   string // the agent's own event name
	Matcher string // the agent's own matcher, "" for none
	Command string
	Timeout int
	// Shell is the shell the command is run in, as a Claude Code plugin's
	// hook names it (pluginHookEnv); "" for none
	Shell string
}

// hookFormat is how one agent's hooks file is laid out: how it is read, how
// one entry is spelled, how a stranger's entry is read back, and when two
// entries are one hook.
type hookFormat interface {
	// Open is the file at path, or why it can't be read as hooks.
	Open(path string) (hookFile, error)
	// Entry is in as the agent's file spells it, or why the agent refuses it.
	Entry(in hookInput) (json.RawMessage, error)
	// Decode is one entry of the file read back into the Library's terms.
	Decode(ev string, e json.RawMessage) (decodedHook, error)
	// Same says whether two entries are one hook: two of the file's, or one
	// just built and one read back from library.json. It opens nothing.
	Same(a, b json.RawMessage) bool
}

// hookFile is an agent's hooks file as a format read it.
type hookFile interface {
	// List is every entry under the agent's event ev, in file order.
	List(ev string) ([]json.RawMessage, error)
	// Lists is every event that has entries, in file order, with them.
	Lists(events []string) ([]string, map[string][]json.RawMessage, error)
	// Write takes the entries at drop's indices out and puts add's in, and
	// leaves every other byte of the file as it is.
	Write(path string, drop map[string][]int, add map[string][]json.RawMessage, lists map[string][]json.RawMessage) error
}

// decodedHook is one entry of an agent's file read back into the Library's
// terms: Timeout in seconds, as the Library keeps it.
type decodedHook struct {
	Matcher string
	// Commands is what the entry runs: a handler that isn't a command is
	// named by its type, "(prompt)"
	Commands []string
	Timeout  int
	// Why is "type", "several" or "shape" when the entry can be listed
	// and removed but not kept as it is written
	Why string
}

// jsonFormat is every agent whose hooks file is JSON: an object holding an
// event's list under "<root>.<Event>", each entry a group of handlers or,
// flat, the handler itself.
type jsonFormat struct {
	ms   bool   // Gemini: timeouts in milliseconds
	flat bool   // Cursor: an entry is the command itself, not a group of them
	root string // the object the lists are in: "" for "hooks", "magpie" for Antigravity
}

// rootKey is the object in the file the events' lists are in.
func (f jsonFormat) rootKey() string {
	if f.root != "" {
		return f.root
	}
	return "hooks"
}

// listPath is where in the file event ev's list is.
func (f jsonFormat) listPath(ev string) string { return f.rootKey() + "." + ev }

func (f jsonFormat) Open(path string) (hookFile, error) {
	doc, err := readHooksFile(path)
	if err != nil {
		return nil, err
	}
	return &jsonFile{jsonFormat: f, path: path, doc: doc}, nil
}

// Entry is in as the file spells it. The unit of the timeout, and whether an
// entry is a group, a flat handler or Antigravity's named one, are the
// format's; which matcher and shell it carries is the spec's, already settled.
func (f jsonFormat) Entry(in hookInput) (json.RawMessage, error) {
	timeout := in.Timeout
	if f.ms {
		timeout *= 1000
	}
	if f.root != "" {
		// Antigravity's: flat handlers, grouped under a matcher for a tool's
		type cmd struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Timeout int    `json:"timeout,omitempty"`
		}
		c := cmd{"command", in.Command, timeout}
		var v any = c
		if in.Event == "PreToolUse" || in.Event == "PostToolUse" {
			m := in.Matcher
			if m == "" {
				m = "*"
			}
			v = struct {
				Matcher string `json:"matcher"`
				Hooks   []cmd  `json:"hooks"`
			}{m, []cmd{c}}
		}
		return json.Marshal(v)
	}
	var v any
	if f.flat {
		v = struct {
			Command string `json:"command"`
			Timeout int    `json:"timeout,omitempty"`
			Matcher string `json:"matcher,omitempty"`
		}{in.Command, timeout, in.Matcher}
	} else {
		type cmd struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Timeout int    `json:"timeout,omitempty"`
			Shell   string `json:"shell,omitempty"`
		}
		v = struct {
			Matcher string `json:"matcher,omitempty"`
			Hooks   []cmd  `json:"hooks"`
		}{in.Matcher, []cmd{{"command", in.Command, timeout, in.Shell}}}
	}
	return json.Marshal(v)
}

// Decode reads a JSON entry back. One that isn't an object with a list of
// handlers where it should have one reads back as no handler at all, which
// is "several": listed, removable, and not kept as it is written.
func (f jsonFormat) Decode(ev string, e json.RawMessage) (decodedHook, error) {
	var group map[string]any
	_ = json.Unmarshal(e, &group)
	if group == nil {
		return decodeHandlers(nil, nil, f.ms), nil
	}
	handlers := []map[string]any{group}
	if !f.flat {
		handlers = handlersOf(group)
	}
	return decodeHandlers(handlers, group["matcher"], f.ms), nil
}

// Same compares these files' entries as they were written, as the engine
// did before formats
func (jsonFormat) Same(a, b json.RawMessage) bool { return canonical(a) == canonical(b) }

// grokFormat is the Grok CLI's config.toml: [[hooks.<Event>]] array tables,
// each with a [[hooks.<Event>.hooks]] child holding the command.
type grokFormat struct{}

func (grokFormat) Open(path string) (hookFile, error) {
	return &grokFile{path: path}, nil
}

// Entry is one block as a JSON string, so it sits in HookEntry.Entry and is
// compared and moved exactly as a JSON agent's entry is.
func (grokFormat) Entry(in hookInput) (json.RawMessage, error) {
	return json.RawMessage(grokHookBlock(in.Event, in.Matcher, in.Command, in.Timeout)), nil
}

// Decode reads one [[hooks.<Event>]] block back: its matcher, and the one
// child holding the command. A block that isn't TOML, or whose child isn't
// there, reads back as no handler at all, which is "several".
func (grokFormat) Decode(ev string, e json.RawMessage) (decodedHook, error) {
	blk, _ := grokBlockText(e)
	var doc map[string]any
	if toml.Unmarshal([]byte(blk), &doc) != nil {
		doc = nil
	}
	hs, _ := doc["hooks"].(map[string]any)
	list, _ := hs[ev].([]any)
	var group map[string]any
	if len(list) == 1 {
		group, _ = list[0].(map[string]any)
	}
	return decodeHandlers(handlersOf(group), group["matcher"], false), nil
}

func (grokFormat) Same(a, b json.RawMessage) bool { return canonical(a) == canonical(b) }

// handlersOf is the handlers a group holds, as JSON and Grok both spell it.
func handlersOf(group map[string]any) []map[string]any {
	var handlers []map[string]any
	if group == nil {
		return nil
	}
	hs, _ := group["hooks"].([]any)
	for _, h := range hs {
		if m, ok := h.(map[string]any); ok {
			handlers = append(handlers, m)
		}
	}
	return handlers
}

// decodeHandlers is what the handlers run, and why the entry is one the
// Library can't keep as it is written, if it is. ms is a file whose timeouts
// are in milliseconds: one that isn't a whole second has no Library timeout
// to be kept as, so it is "shape".
func decodeHandlers(handlers []map[string]any, matcher any, ms bool) decodedHook {
	d := decodedHook{Commands: []string{}}
	d.Matcher, _ = matcher.(string)
	why := ""
	for _, h := range handlers {
		typ, _ := h["type"].(string)
		cmd, _ := h["command"].(string)
		if (typ == "" || typ == "command") && cmd != "" {
			d.Commands = append(d.Commands, cmd)
		} else {
			if typ == "" {
				typ = "?"
			}
			d.Commands = append(d.Commands, "("+typ+")")
			why = "type"
		}
	}
	if len(handlers) == 1 {
		d.Timeout = numberOf(handlers[0]["timeout"])
		if ms && d.Timeout%1000 != 0 {
			// not a whole second: it has no Library timeout to be kept as
			why = "shape"
		} else if ms {
			d.Timeout /= 1000
		}
	}
	switch {
	case why != "":
	case len(handlers) != 1:
		why = "several"
	}
	d.Why = why
	return d
}

// jsonFile is a JSON hooks file as jsonFormat.Open read it: its document,
// with the comments and spacing the user gave it.
type jsonFile struct {
	jsonFormat
	path string
	doc  []byte
}

// List is the list the file has under the event's path, or why it can't be
// read as one.
func (f *jsonFile) List(ev string) ([]json.RawMessage, error) {
	return hookListAt(f.doc, f.path, f.listPath(ev))
}

// Lists is every event's list in the file, the events in the file's order.
// events is not what decides them: the file says what it holds, and a key
// of the root that isn't a list is left out, as nothing the agent runs.
func (f *jsonFile) Lists(events []string) ([]string, map[string][]json.RawMessage, error) {
	lists := map[string][]json.RawMessage{}
	if f.doc == nil {
		return nil, lists, nil
	}
	var evs []string
	var failed error
	gjson.GetBytes(f.doc, f.rootKey()).ForEach(func(k, v gjson.Result) bool {
		if !v.IsArray() {
			return true // not a list of hooks: nothing the agent runs, left as it is
		}
		var list []json.RawMessage
		if err := json.Unmarshal([]byte(v.Raw), &list); err != nil {
			failed = err
			return false
		}
		evs = append(evs, k.String())
		lists[k.String()] = list
		return true
	})
	return evs, lists, failed
}

// Write is never called on a JSON file: the sync writes it itself, moving
// Codex's trust records with the groups it moves (hookWrite.run).
func (f *jsonFile) Write(string, map[string][]int, map[string][]json.RawMessage, map[string][]json.RawMessage) error {
	return errors.New("a JSON hooks file is written by the sync, not by its format")
}

// wantsVersion is whether the file carries a "version" key magpie keeps at 1
// (Cursor's hooks.json).
func (f *jsonFile) wantsVersion() bool { return f.flat }

// namedHooksFile is a hooks file whose top-level keys are named hooks
// (Antigravity's), so an entry magpie would write may already run under a
// hook of the user's. Only a file with a root is one: elsewhere magpie's
// lists are the file's, and find already saw every entry of them.
type namedHooksFile interface {
	// namedHas is whether a hook other than magpie's own already runs entry
	// at event ev: the user's or the repository's. It isn't written again,
	// or the command would run twice (#7507).
	namedHas(ev string, entry json.RawMessage) bool
}

func (f *jsonFile) namedHas(ev string, entry json.RawMessage) bool {
	return f.root != "" && namedHas(f.doc, f.root, ev, entry)
}

// grokFile is a Grok config.toml as the format opened it.
type grokFile struct {
	path string
}

// List is the [[hooks.<ev>]] blocks a Grok config.toml has, each as the block
// text it is, so it lines up with what grokHookBlock records.
func (f *grokFile) List(ev string) ([]json.RawMessage, error) {
	return grokHookBlocks(f.path, ev)
}

// Lists is every event of events that has blocks, in events' order: the
// file names what it holds only by the blocks' headers, so the agent's own
// events are the ones looked for.
func (f *grokFile) Lists(events []string) ([]string, map[string][]json.RawMessage, error) {
	lists := map[string][]json.RawMessage{}
	var evs []string
	for _, ev := range events {
		x, err := grokHookBlocks(f.path, ev)
		if err != nil {
			return nil, nil, err
		}
		if len(x) > 0 {
			evs = append(evs, ev)
			lists[ev] = x
		}
	}
	return evs, lists, nil
}

// Write takes the blocks at drop's indices out and puts add's in, the
// user's own blocks, comments and other settings left as they are.
func (f *grokFile) Write(path string, drop map[string][]int, add map[string][]json.RawMessage, lists map[string][]json.RawMessage) error {
	var drops, adds [][]byte
	for ev, idxs := range drop {
		for _, i := range idxs {
			if blk, ok := grokBlockText(lists[ev][i]); ok {
				drops = append(drops, []byte(blk))
			}
		}
	}
	for _, es := range add {
		for _, e := range es {
			if blk, ok := grokBlockText(e); ok {
				adds = append(adds, []byte(blk))
			}
		}
	}
	return writeGrokHooks(path, drops, adds)
}
