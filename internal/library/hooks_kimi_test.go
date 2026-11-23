package library

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Kimi Code refuses a timeout it does not take, and never clamps one: 0 is
// its own default and is not written, 1..600 is written as written.
func TestKimiTimeoutRange(t *testing.T) {
	for _, tc := range []struct {
		in        int
		wantError string
		wantSet   bool
		wantN     int
	}{
		{in: 0},
		{in: 1, wantSet: true, wantN: 1},
		{in: 600, wantSet: true, wantN: 600},
		{in: 601, wantError: "Kimi Code takes a hook's timeout from 1 to 600 seconds, not 601"},
		{in: 700, wantError: "Kimi Code takes a hook's timeout from 1 to 600 seconds, not 700"},
		{in: 86400, wantError: "Kimi Code takes a hook's timeout from 1 to 600 seconds, not 86400"},
		{in: -1, wantError: "Kimi Code takes a hook's timeout from 1 to 600 seconds, not -1"},
	} {
		got, err := newKimiTimeout(tc.in)
		switch {
		case tc.wantError != "":
			if err == nil || err.Error() != tc.wantError {
				t.Errorf("timeout %d: got err %v, want %q", tc.in, err, tc.wantError)
			}
			if got.set {
				t.Errorf("timeout %d: a refused one came back set", tc.in)
			}
		default:
			if err != nil {
				t.Errorf("timeout %d: %v", tc.in, err)
			}
			if got.set != tc.wantSet || (got.set && got.n != tc.wantN) {
				t.Errorf("timeout %d: got %+v, want set=%v n=%d", tc.in, got, tc.wantSet, tc.wantN)
			}
		}
	}
}

// A hook is refused for the reason Kimi would refuse the file: an event it
// does not fire, a command that is empty, a timeout out of range.
func TestKimiEntryRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   hookInput
		want string
	}{
		{"unknown event", hookInput{Event: "OnSomething", Command: "true"}, "Kimi Code has no hook event called OnSomething"},
		{"empty event", hookInput{Event: "", Command: "true"}, "Kimi Code has no hook event called "},
		{"Grok's own name", hookInput{Event: "PermissionDenied", Command: "true"}, "Kimi Code has no hook event called PermissionDenied"},
		{"empty command", hookInput{Event: "Stop"}, "a hook needs a command to run"},
		{"blank command", hookInput{Event: "Stop", Command: " \t\n"}, "a hook needs a command to run"},
		{"timeout", hookInput{Event: "Stop", Command: "true", Timeout: 601}, "Kimi Code takes a hook's timeout from 1 to 600 seconds, not 601"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := newKimiEntry(tc.in, kimiHookEvents)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got err %v, want %q", err, tc.want)
			}
			if e != (kimiEntry{}) {
				t.Errorf("a refused hook came back as an entry: %+v", e)
			}
		})
	}
	// A list other than kimiHookEvents decides what is named.
	if _, err := newKimiEntry(hookInput{Event: "Stop", Command: "true"}, []kimiEvent{"PreToolUse"}); err == nil ||
		err.Error() != "Kimi Code has no hook event called Stop" {
		t.Errorf("events given: %v", err)
	}
}

// The block is spelled as Kimi's documentation spells it: header, event,
// matcher only when there is one, command, timeout only when one is set.
func TestKimiEntryBlock(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   hookInput
		want string
	}{
		{"every key", hookInput{Event: "PreToolUse", Matcher: "Bash", Command: `say "hi"`, Timeout: 30},
			"[[hooks]]\nevent = \"PreToolUse\"\nmatcher = \"Bash\"\ncommand = \"say \\\"hi\\\"\"\ntimeout = 30\n"},
		{"no matcher, no timeout", hookInput{Event: "Stop", Command: "say done"},
			"[[hooks]]\nevent = \"Stop\"\ncommand = \"say done\"\n"},
		{"timeout 0 is not written", hookInput{Event: "Stop", Command: "say done", Timeout: 0},
			"[[hooks]]\nevent = \"Stop\"\ncommand = \"say done\"\n"},
		{"timeout 1 and 600 are written", hookInput{Event: "Stop", Command: "true", Timeout: 600},
			"[[hooks]]\nevent = \"Stop\"\ncommand = \"true\"\ntimeout = 600\n"},
		{"matcher on a session start", hookInput{Event: "SessionStart", Matcher: "startup", Command: "true"},
			"[[hooks]]\nevent = \"SessionStart\"\nmatcher = \"startup\"\ncommand = \"true\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := newKimiEntry(tc.in, kimiHookEvents)
			if err != nil {
				t.Fatal(err)
			}
			got, err := e.block()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("block:\n got %q\nwant %q", got, tc.want)
			}
			// a final newline and no indent, as the file's other rules are
			if !strings.HasSuffix(got, "\n") || strings.Contains(got, "\n ") || strings.Contains(got, "\n\t") {
				t.Errorf("block is indented or unterminated: %q", got)
			}
		})
	}
	// timeout is a bare integer. Kimi refuses it quoted ("5"), so it is
	// never spelled as a string.
	for _, n := range []int{1, 30, 600} {
		e, err := newKimiEntry(hookInput{Event: "Stop", Command: "true", Timeout: n}, kimiHookEvents)
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.block()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(got, "timeout = "+strconv.Itoa(n)+"\n") || strings.Contains(got, `timeout = "`) {
			t.Errorf("timeout %d is not a bare integer:\n%s", n, got)
		}
	}
}

// An entry assembled by hand inside the package never renders a block Kimi
// would reject the whole config.toml for. block() refuses it with the
// boundary's own words for an unknown event, an empty command, or a set
// timeout outside 1..600; a set timeout of 0 it renders as no timeout.
func TestKimiEntryBlockRevalidates(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    kimiEntry
		want string
	}{
		{"unknown event", kimiEntry{event: "Nope", command: "true"}, "Kimi Code has no hook event called Nope"},
		{"empty command", kimiEntry{event: "Stop", command: "  "}, "a hook needs a command to run"},
		{"timeout above range", kimiEntry{event: "Stop", command: "true", timeout: kimiTimeout{set: true, n: 601}},
			"Kimi Code takes a hook's timeout from 1 to 600 seconds, not 601"},
		{"timeout negative", kimiEntry{event: "Stop", command: "true", timeout: kimiTimeout{set: true, n: -5}},
			"Kimi Code takes a hook's timeout from 1 to 600 seconds, not -5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.e.block()
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got (%q, %v), want error %q", got, err, tc.want)
			}
			if got != "" {
				t.Errorf("a refused entry rendered %q", got)
			}
		})
	}
	// A set timeout is read through newKimiTimeout, which makes 0 the
	// unset one: so {set, 0} renders with no timeout key rather than as a
	// 0 Kimi refuses. Unset with any n beside it is unset.
	for _, timeout := range []kimiTimeout{{set: true, n: 0}, {set: false, n: 999}, {set: false, n: -5}} {
		e := kimiEntry{event: "Stop", command: "true", timeout: timeout}
		got, err := e.block()
		if err != nil || strings.Contains(got, "timeout") {
			t.Errorf("timeout %+v rendered %q, %v", timeout, got, err)
		}
	}
}

// kimiEntry has exactly the keys Kimi accepts and no room for a fifth.
func TestKimiEntryHasExactlyKimiKeys(t *testing.T) {
	want := []string{"event", "matcher", "command", "timeout"}
	var got []string
	for _, f := range reflect.VisibleFields(reflect.TypeOf(kimiEntry{})) {
		if f.PkgPath != "" { // unexported, as every field is
			got = append(got, f.Name)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kimiEntry's fields are %v, want %v", got, want)
	}
	if n := reflect.TypeOf(kimiEntry{}).NumField(); n != len(want) {
		t.Errorf("kimiEntry has %d fields, want %d", n, len(want))
	}
}

// kimiHookEvents names the twenty events Kimi's validator names, in its
// order, and each is one kimiEvent.
func TestKimiHookEventsAreKimiCodes(t *testing.T) {
	want := []kimiEvent{
		"PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest",
		"PermissionResult", "UserPromptSubmit", "UserPromptQueued", "TurnStarted",
		"Stop", "StopFailure", "Interrupt", "SessionStart", "SessionEnd",
		"SessionHeartbeat", "SubagentStart", "SubagentStop", "TaskStarted",
		"PreCompact", "PostCompact", "Notification",
	}
	if !reflect.DeepEqual(kimiHookEvents, want) {
		t.Fatalf("events:\n got %v\nwant %v", kimiHookEvents, want)
	}
	if len(kimiHookEvents) != 20 {
		t.Errorf("%d events, want 20", len(kimiHookEvents))
	}
}

// tomlString writes a TOML 1.0 basic string and no escape TOML refuses: the
// six short escapes, \uXXXX for every other control character, and the rest
// of the text as UTF-8. The expected output is written here by hand.
func TestTomlString(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"plain", `say done`, `"say done"`},
		{"empty", ``, `""`},
		{"newline", "a\nb", `"a\nb"`},
		{"carriage return", "a\rb", `"a\rb"`},
		{"tab", "a\tb", `"a\tb"`},
		{"backspace", "a\bb", `"a\bb"`},
		{"form feed", "a\fb", `"a\fb"`},
		{"double quote", `say "hi"`, `"say \"hi\""`},
		{"backslash", `a\b`, `"a\\b"`},
		{"U+0001", "a\x01b", `"a\u0001b"`},
		{"U+001F", "a\x1fb", `"a\u001Fb"`},
		{"U+007F", "a\x7fb", `"a\u007Fb"`},
		{"U+200B is written raw", "a\u200bb", "\"a\u200bb\""},
		{"emoji is written raw", "a🚀b", "\"a🚀b\""},
		{"CJK is written raw", "中文", `"中文"`},
		{"the escapes together, in one command", "x\ty\nz\"w\\v\b\f\r\x01\x7f\u200b🚀",
			`"x\ty\nz\"w\\v\b\f\r\u0001\u007F` + "\u200b🚀\""},
		{"a literal backslash is doubled, never passed as an escape", `\n`, `"\\n"`},
		{"a literal \\x01 is two characters, not an escape", `\x01`, `"\\x01"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tomlString(tc.in); got != tc.want {
				t.Fatalf("tomlString(%q):\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}
