package library

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

// Kimi Code keeps hooks as [[hooks]] array tables in its config.toml, one
// rule each. The shape is strict as no other agent's here is: one key or
// value Kimi does not take and it refuses the whole config.toml, models and
// providers with the hooks. So a hook crosses one boundary, newKimiEntry,
// and past it a kimiEntry is something Kimi accepts.

// kimiFormat is Kimi Code's config.toml as a hooks file: [[hooks]] array
// tables, each one rule. The blocks are flatTOML's; what a block means is
// Kimi's, and lives here.
var kimiFormat = flatTOML{
	table: "hooks",
	event: "event",
	encode: func(in hookInput) (string, error) {
		e, err := newKimiEntry(in, kimiHookEvents)
		if err != nil {
			return "", err
		}
		return e.block()
	},
	decode: kimiDecode,
	same:   kimiSameBlock,
}

// kimiDecode reads one [[hooks]] block back. A block with any key but
// Kimi's four, a value of the wrong sort, or an event other than ev is one
// Kimi would refuse: it is listed and can be taken out, but the Library
// can't keep it as it is written, so it reads back as "shape", with what it
// could read of it. Kimi's timeout is in seconds, the Library's own unit,
// and one the block doesn't set is 0.
func kimiDecode(ev, block string) (decodedHook, error) {
	e, ok := kimiBlock(block)
	d := decodedHook{Matcher: e.matcher, Commands: []string{}, Timeout: e.timeoutSeconds()}
	if e.command != "" {
		d.Commands = []string{e.command}
	}
	if !ok || string(e.event) != ev {
		d.Why = "shape"
	}
	return d, nil
}

// kimiSameBlock is whether two blocks are one hook: both read as Kimi's four
// keys and compared field by field, so a block the user's tool saved again
// with another key order or indent is still the one magpie wrote. A block
// that isn't Kimi's is compared by its exact text.
func kimiSameBlock(a, b string) bool {
	x, aOK := kimiBlock(a)
	y, bOK := kimiBlock(b)
	if !aOK || !bOK {
		return a == b
	}
	return x == y
}

// kimiBlock is one [[hooks]] block read into Kimi's keys, and whether it is
// one Kimi takes: exactly a command and an event, each a string, with
// optionally a matcher and a timeout from 1 to 600, and no other key. What
// could be read of a block Kimi refuses is returned with it.
func kimiBlock(block string) (kimiEntry, bool) {
	// the block is a [[hooks]] table, so its keys are under that one table
	var doc struct {
		Hooks []map[string]any `toml:"hooks"`
	}
	if toml.Unmarshal([]byte(block), &doc) != nil || len(doc.Hooks) != 1 {
		return kimiEntry{}, false
	}
	var e kimiEntry
	ok := true
	for k, v := range doc.Hooks[0] {
		s, isString := v.(string)
		n, isInt := v.(int64)
		switch k {
		case "event":
			e.event, ok = kimiEvent(s), ok && isString
		case "matcher":
			e.matcher, ok = s, ok && isString
		case "command":
			e.command, ok = s, ok && isString
		case "timeout":
			inRange := isInt && n >= 1 && n <= 600
			if inRange {
				e.timeout = kimiTimeout{set: true, n: int(n)}
			}
			ok = ok && inRange
		default:
			ok = false
		}
	}
	// Kimi refuses a rule with no command, and one with no event
	if e.event == "" || e.command == "" {
		ok = false
	}
	return e, ok
}

// timeoutSeconds is the block's timeout in the Library's seconds, 0 for none.
func (e kimiEntry) timeoutSeconds() int {
	if !e.timeout.set {
		return 0
	}
	return e.timeout.n
}

// kimiEvent is one of the events Kimi Code fires. Only kimiHookEvents names
// them, so a value of this type is one Kimi knows.
type kimiEvent string

// kimiHookEvents are the events Kimi Code fires, in the order its own
// validator names them when one is unknown (kimi doctor, kimi 2.7.2).
var kimiHookEvents = []kimiEvent{
	"PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest",
	"PermissionResult", "UserPromptSubmit", "UserPromptQueued", "TurnStarted",
	"Stop", "StopFailure", "Interrupt", "SessionStart", "SessionEnd",
	"SessionHeartbeat", "SubagentStart", "SubagentStop", "TaskStarted",
	"PreCompact", "PostCompact", "Notification",
}

// kimiTimeout is a hook's timeout as Kimi Code takes it: unset, or a whole
// second from 1 to 600. newKimiTimeout is the only way to make one, and it
// never makes a set one outside that range: there is nothing for a fraction
// or a number Kimi refuses. A 0 in the Library's terms is the unset one.
type kimiTimeout struct {
	set bool
	n   int
}

// kimiEntry is one [[hooks]] rule. It has exactly the four keys Kimi accepts
// and no others, and only newKimiEntry makes one.
type kimiEntry struct {
	event   kimiEvent
	matcher string
	command string
	timeout kimiTimeout
}

// newKimiTimeout is t as Kimi Code takes it, or why it refuses it. 0 is the
// agent's own default and is not written. Nothing is clamped, ever: a
// deadline the user did not set is not written in their name.
func newKimiTimeout(t int) (kimiTimeout, error) {
	switch {
	case t == 0:
		return kimiTimeout{}, nil
	case t < 1 || t > 600:
		return kimiTimeout{}, fmt.Errorf("Kimi Code takes a hook's timeout from 1 to 600 seconds, not %d", t)
	}
	return kimiTimeout{set: true, n: t}, nil
}

// newKimiEntry is in as one [[hooks]] rule Kimi Code accepts, or why Kimi
// refuses it: its timeout first, then an event events does not name, then a
// command that is empty once trimmed.
func newKimiEntry(in hookInput, events []kimiEvent) (kimiEntry, error) {
	timeout, err := newKimiTimeout(in.Timeout)
	if err != nil {
		return kimiEntry{}, err
	}
	if !slices.Contains(events, kimiEvent(in.Event)) {
		return kimiEntry{}, fmt.Errorf("Kimi Code has no hook event called %s", in.Event)
	}
	if strings.TrimSpace(in.Command) == "" {
		return kimiEntry{}, errors.New("a hook needs a command to run")
	}
	return kimiEntry{event: kimiEvent(in.Event), matcher: in.Matcher, command: in.Command, timeout: timeout}, nil
}

// block is the entry as a [[hooks]] table, with a final newline and no
// indent: event, then matcher only when non-empty, then command, then
// timeout only when set. The receiver is validated again first, against
// kimiHookEvents itself, so an entry assembled by hand in the package cannot
// render what Kimi refuses.
func (e kimiEntry) block() (string, error) {
	if !slices.Contains(kimiHookEvents, e.event) {
		return "", fmt.Errorf("Kimi Code has no hook event called %s", e.event)
	}
	if strings.TrimSpace(e.command) == "" {
		return "", errors.New("a hook needs a command to run")
	}
	var timeout kimiTimeout
	if e.timeout.set {
		var err error
		if timeout, err = newKimiTimeout(e.timeout.n); err != nil {
			return "", err
		}
	}
	var b strings.Builder
	b.WriteString("[[hooks]]\n")
	b.WriteString("event = " + tomlString(string(e.event)) + "\n")
	if e.matcher != "" {
		b.WriteString("matcher = " + tomlString(e.matcher) + "\n")
	}
	b.WriteString("command = " + tomlString(e.command) + "\n")
	if timeout.set {
		b.WriteString("timeout = " + strconv.Itoa(timeout.n) + "\n")
	}
	return b.String(), nil
}

// tomlString spells s as a TOML 1.0 basic string. strconv.Quote is not a
// substitute: it writes a control byte as \x01 and a vertical tab as \v.
// Kimi's own validator takes \x01 and magpie's (go-toml) refuses it, and
// both refuse \v; so such a value would be a file magpie cannot write, and
// the sync of that agent would fail every time. Only what both take is
// written: the six short escapes, \uXXXX for every other control character
// (which TOML refuses raw), and the rest of the text as UTF-8, non-printing
// runes and all.
func tomlString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == utf8.RuneError && size == 1 {
			// A byte that is not part of a rune: TOML has no escape for a
			// single byte, only for code points. Left as it is, so the file
			// is refused on write rather than quietly changed to U+FFFD.
			b.WriteByte(s[i-size])
			continue
		}
		switch r {
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
