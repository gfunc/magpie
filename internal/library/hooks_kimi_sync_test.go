package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A Kimi config.toml as the user has it before magpie writes into it: their
// own tables, a comment, and two [[hooks]] blocks in the order and style
// Kimi's own tool writes them.
const kimiSeed = `# my Kimi Code config
default_model = "kimi-code/k2.7"

[thinking]
effort = "high"

# said when a turn ends
[[hooks]]
event = "Stop"
command = "say done"

[[hooks]]
event = "PostToolUse"
matcher = "Bash"
command = "my-log"
`

// kimiFile writes seed into a config.toml of its own and gives its path.
func kimiFile(t *testing.T, seed string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, seed)
	return path
}

// kimiHere puts Kimi Code in a sandbox, and gives the hooks file its spec
// names. Kimi is a target from its ~/.kimi-code folder alone, with no CLI
// stand-in; it isn't in every sandbox, because it reads ~/.agents/skills
// too, which the skills tests count.
func kimiHere(t *testing.T, h string, seed string) string {
	t.Helper()
	path := filepath.Join(h, ".kimi-code", "config.toml")
	write(t, path, seed)
	return path
}

// kimiBlocks is the file's [[hooks]] blocks, as text, in file order. Only
// the events the test names are read: one a block holds that isn't among
// them is not looked for.
func kimiBlocks(t *testing.T, path string) []string {
	t.Helper()
	file, err := kimiFormat.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var out []string
	for _, ev := range []string{"PreToolUse", "PostToolUse", "Stop", "SessionStart", "UserPromptSubmit"} {
		list, err := file.List(ev)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			out = append(out, mustBlock(t, e))
		}
	}
	return out
}

func mustBlock(t *testing.T, e json.RawMessage) string {
	t.Helper()
	s, ok := blockText(e)
	if !ok {
		t.Fatalf("entry %s isn't a block", e)
	}
	return s
}

// kimiWrite is what the sync asks of a Kimi file it has opened: the entries
// at drop's indices out, add's in. drop's indices are into the list each
// event has in this read, so the lists are taken from the same file and
// handed over with them, as the sync does.
func kimiWrite(t *testing.T, f hookFile, path string, drop map[string][]int, add map[string][]json.RawMessage) error {
	t.Helper()
	_, lists, err := f.Lists([]string{"PreToolUse", "PostToolUse", "Stop", "SessionStart", "UserPromptSubmit"})
	if err != nil {
		t.Fatal(err)
	}
	return f.Write(path, drop, add, lists)
}

// kimiBlockOf is one hook as magpie writes it for Kimi, through the format.
func kimiBlockOf(t *testing.T, in hookInput) json.RawMessage {
	t.Helper()
	e, err := kimiFormat.Entry(in)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// Open reads what is there and no more. A missing file is an empty one. A
// block's text is the file's own lines, so the user's comment inside a block
// and their spelling of a value come back as they wrote them. A block with no
// event key, or one that isn't a string, is in no list.
func TestKimiFormatOpenListsTheFile(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		f, err := kimiFormat.Open(filepath.Join(t.TempDir(), "none", "config.toml"))
		if err != nil {
			t.Fatalf("a missing file: %v", err)
		}
		evs, lists, err := f.Lists([]string{"Stop"})
		if err != nil || len(evs) != 0 || len(lists) != 0 {
			t.Errorf("a missing file lists %v %v, %v", evs, lists, err)
		}
	})
	path := kimiFile(t, "# mine\n[[hooks]]\nevent = 'Stop'\n# a note of mine\ncommand = \"\"\"\necho one\necho two\n\"\"\"\n\n[[hooks]]\ncommand = \"no event\"\n\n[[hooks]]\nevent = 5\ncommand = \"a number\"\n")
	got := kimiBlocks(t, path)
	want := []string{"[[hooks]]\nevent = 'Stop'\n# a note of mine\ncommand = \"\"\"\necho one\necho two\n\"\"\"\n"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("blocks:\n got %q\nwant %q", got, want)
	}
}

// A file that isn't TOML is refused and left as it is. The same holds when
// "hooks" is something other than an array of tables.
func TestKimiFormatOpenRefusesWhatItCantRead(t *testing.T) {
	for name, src := range map[string]string{
		"not toml":           "default_model = \n",
		"hooks is a table":   "[hooks]\nevent = \"Stop\"\n",
		"hooks is a value":   "hooks = []\n",
		"duplicate in block": "[[hooks]]\nevent = \"Stop\"\nevent = \"Stop\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := kimiFile(t, src)
			if _, err := kimiFormat.Open(path); err == nil {
				t.Fatalf("opened %q", src)
			}
			if got := read(t, path); got != src {
				t.Errorf("the file was touched:\n%s", got)
			}
		})
	}
}

// Entry is the block as Kimi documents it, held as a JSON string.
func TestKimiFormatEntry(t *testing.T) {
	got := string(kimiBlockOf(t, hookInput{Event: "PreToolUse", Matcher: "Bash", Command: "say done", Timeout: 10}))
	if want := `"[[hooks]]\nevent = \"PreToolUse\"\nmatcher = \"Bash\"\ncommand = \"say done\"\ntimeout = 10\n"`; got != want {
		t.Errorf("entry:\n got %s\nwant %s", got, want)
	}
	if _, err := kimiFormat.Entry(hookInput{Event: "Stop", Command: "x", Timeout: 700}); err == nil ||
		err.Error() != "Kimi Code takes a hook's timeout from 1 to 600 seconds, not 700" {
		t.Errorf("timeout 700: %v", err)
	}
}

// Decode reads back what the Library keeps of a block. One Kimi would refuse
// is read as far as it can be and said "shape".
func TestKimiFormatDecode(t *testing.T) {
	for _, tc := range []struct {
		name, block string
		want        decodedHook
	}{
		{"plain", "[[hooks]]\nevent = \"Stop\"\ncommand = \"say done\"\n", decodedHook{Commands: []string{"say done"}}},
		{"every key", "[[hooks]]\nevent = \"PreToolUse\"\nmatcher = \"Bash\"\ncommand = \"x\"\ntimeout = 10\n",
			decodedHook{Matcher: "Bash", Commands: []string{"x"}, Timeout: 10}},
		{"literal strings", "[[hooks]]\nevent = 'Stop'\ncommand = 'say done'\n", decodedHook{Commands: []string{"say done"}}},
		{"a fifth key", "[[hooks]]\nevent = \"Stop\"\ncommand = \"x\"\nenabled = true\n",
			decodedHook{Commands: []string{"x"}, Why: "shape"}},
		{"timeout out of range", "[[hooks]]\nevent = \"Stop\"\ncommand = \"x\"\ntimeout = 0\n",
			decodedHook{Commands: []string{"x"}, Why: "shape"}},
		{"timeout quoted", "[[hooks]]\nevent = \"Stop\"\ncommand = \"x\"\ntimeout = \"5\"\n",
			decodedHook{Commands: []string{"x"}, Why: "shape"}},
		{"no command", "[[hooks]]\nevent = \"Stop\"\n", decodedHook{Commands: []string{}, Why: "shape"}},
		{"not toml", "[[hooks]]\nevent = \n", decodedHook{Commands: []string{}, Why: "shape"}},
		{"two tables in one entry", "[[hooks]]\nevent = \"Stop\"\ncommand = \"x\"\n[[hooks]]\nevent = \"Stop\"\ncommand = \"y\"\n",
			decodedHook{Commands: []string{}, Why: "shape"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := "Stop"
			if strings.Contains(tc.block, "PreToolUse") {
				ev = "PreToolUse"
			}
			got, err := kimiFormat.Decode(ev, jsonString(tc.block))
			if err != nil {
				t.Fatalf("decode said %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("decoded:\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
	// a block under another event than the list's is Kimi's refusal too
	got, _ := kimiFormat.Decode("PostToolUse", jsonString("[[hooks]]\nevent = \"Stop\"\ncommand = \"x\"\n"))
	if got.Why != "shape" {
		t.Errorf("a Stop block read as PostToolUse: %+v", got)
	}
}

// Two blocks are one hook when their four keys are, whatever the key order
// or whitespace. One that isn't Kimi's is compared by its text.
func TestKimiFormatSame(t *testing.T) {
	mine := "[[hooks]]\nevent = \"PreToolUse\"\nmatcher = \"Bash\"\ncommand = \"say done\"\ntimeout = 10\n"
	for _, tc := range []struct {
		name string
		a, b string
		want bool
	}{
		{"the same text", mine, mine, true},
		{"re-saved: another order and indent", mine, "[[hooks]]\n    timeout = 10\n    command = \"say done\"\n    event = \"PreToolUse\"\n    matcher = \"Bash\"\n", true},
		{"re-saved: a literal string", mine, "[[hooks]]\nevent = \"PreToolUse\"\nmatcher = 'Bash'\ncommand = 'say done'\ntimeout = 10\n", true},
		{"no timeout either side", "[[hooks]]\nevent = \"Stop\"\ncommand = \"x\"\n", "[[hooks]]\ncommand = \"x\"\nevent = \"Stop\"\n", true},
		{"another command", mine, "[[hooks]]\nevent = \"PreToolUse\"\nmatcher = \"Bash\"\ncommand = \"say again\"\ntimeout = 10\n", false},
		{"another timeout", mine, "[[hooks]]\nevent = \"PreToolUse\"\nmatcher = \"Bash\"\ncommand = \"say done\"\ntimeout = 11\n", false},
		{"timeout unset against set", "[[hooks]]\nevent = \"Stop\"\ncommand = \"x\"\n", "[[hooks]]\nevent = \"Stop\"\ncommand = \"x\"\ntimeout = 30\n", false},
		{"another event", mine, "[[hooks]]\nevent = \"PostToolUse\"\nmatcher = \"Bash\"\ncommand = \"say done\"\ntimeout = 10\n", false},
		{"a fifth key is its text", "[[hooks]]\nevent = \"Stop\"\ncommand = \"x\"\nextra = 1\n", "[[hooks]]\nevent = \"Stop\"\nextra = 1\ncommand = \"x\"\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := kimiFormat.Same(jsonString(tc.a), jsonString(tc.b)); got != tc.want {
				t.Errorf("Same = %v, want %v", got, tc.want)
			}
		})
	}
	// an entry that isn't a block is compared as it is
	if kimiFormat.Same(json.RawMessage(`{"a":1}`), json.RawMessage(`{"b":1}`)) ||
		!kimiFormat.Same(json.RawMessage(`{"a":1}`), json.RawMessage(`{ "a": 1 }`)) {
		t.Error("a non-block entry isn't compared canonically")
	}
}

// Write is a splice over the file's own lines. A block kept is copied as the
// file has it, one dropped takes the blank lines before it and nothing of
// its neighbours, and one added goes after the last block of the table.
func TestKimiFormatWriteSplicesTheFile(t *testing.T) {
	t.Run("add to a file with blocks, after the last one", func(t *testing.T) {
		path := kimiFile(t, kimiSeed)
		f, err := kimiFormat.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		add := map[string][]json.RawMessage{"Stop": {kimiBlockOf(t, hookInput{Event: "Stop", Command: "bark", Timeout: 10})}}
		if err := kimiWrite(t, f, path, map[string][]int{}, add); err != nil {
			t.Fatal(err)
		}
		got := read(t, path)
		want := kimiSeed + "\n[[hooks]]\nevent = \"Stop\"\ncommand = \"bark\"\ntimeout = 10\n"
		if got != want {
			t.Errorf("file:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("add to a file with no hooks table, at the end", func(t *testing.T) {
		path := kimiFile(t, "default_model = \"kimi-code/k2.7\"\n\n[thinking]\neffort = \"high\"\n")
		f, _ := kimiFormat.Open(path)
		add := map[string][]json.RawMessage{"Stop": {kimiBlockOf(t, hookInput{Event: "Stop", Command: "bark"})}}
		if err := kimiWrite(t, f, path, map[string][]int{}, add); err != nil {
			t.Fatal(err)
		}
		want := "default_model = \"kimi-code/k2.7\"\n\n[thinking]\neffort = \"high\"\n\n[[hooks]]\nevent = \"Stop\"\ncommand = \"bark\"\n"
		if got := read(t, path); got != want {
			t.Errorf("file:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("add to an empty file", func(t *testing.T) {
		path := kimiFile(t, "")
		f, _ := kimiFormat.Open(path)
		add := map[string][]json.RawMessage{"Stop": {kimiBlockOf(t, hookInput{Event: "Stop", Command: "bark"})}}
		if err := kimiWrite(t, f, path, map[string][]int{}, add); err != nil {
			t.Fatal(err)
		}
		if got := read(t, path); got != "[[hooks]]\nevent = \"Stop\"\ncommand = \"bark\"\n" {
			t.Errorf("file: %q", got)
		}
	})

	t.Run("add to a file that doesn't exist yet", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := kimiFormat.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		add := map[string][]json.RawMessage{"Stop": {kimiBlockOf(t, hookInput{Event: "Stop", Command: "bark"})}}
		if err := kimiWrite(t, f, path, map[string][]int{}, add); err != nil {
			t.Fatal(err)
		}
		if got := read(t, path); got != "[[hooks]]\nevent = \"Stop\"\ncommand = \"bark\"\n" {
			t.Errorf("file: %q", got)
		}
	})

	t.Run("drop one of the user's, in the middle", func(t *testing.T) {
		// the user's two, then magpie's, then a table after
		path := kimiFile(t, "# mine\n[[hooks]]\nevent = \"Stop\"\ncommand = \"one\"\n\n[[hooks]]\nevent = \"Stop\"\ncommand = \"two\"\n\n[[hooks]]\nevent = \"PostToolUse\"\ncommand = \"three\"\n\n[ui]\ntheme = \"dark\"\n")
		f, _ := kimiFormat.Open(path)
		if err := kimiWrite(t, f, path, map[string][]int{"Stop": {1}}, nil); err != nil {
			t.Fatal(err)
		}
		want := "# mine\n[[hooks]]\nevent = \"Stop\"\ncommand = \"one\"\n\n[[hooks]]\nevent = \"PostToolUse\"\ncommand = \"three\"\n\n[ui]\ntheme = \"dark\"\n"
		if got := read(t, path); got != want {
			t.Errorf("file:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("drop the last one, the table after stays", func(t *testing.T) {
		path := kimiFile(t, "[[hooks]]\nevent = \"Stop\"\ncommand = \"one\"\n\n[[hooks]]\nevent = \"Stop\"\ncommand = \"two\"\n\n[ui]\ntheme = \"dark\"\n")
		f, _ := kimiFormat.Open(path)
		if err := kimiWrite(t, f, path, map[string][]int{"Stop": {1}}, nil); err != nil {
			t.Fatal(err)
		}
		want := "[[hooks]]\nevent = \"Stop\"\ncommand = \"one\"\n\n[ui]\ntheme = \"dark\"\n"
		if got := read(t, path); got != want {
			t.Errorf("file:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("drop the first, the comment above it stays", func(t *testing.T) {
		path := kimiFile(t, "# mine\n[[hooks]]\nevent = \"Stop\"\ncommand = \"one\"\n\n[[hooks]]\nevent = \"Stop\"\ncommand = \"two\"\n")
		f, _ := kimiFormat.Open(path)
		if err := kimiWrite(t, f, path, map[string][]int{"Stop": {0}}, nil); err != nil {
			t.Fatal(err)
		}
		want := "# mine\n\n[[hooks]]\nevent = \"Stop\"\ncommand = \"two\"\n"
		if got := read(t, path); got != want {
			t.Errorf("file:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("drop and add in one write", func(t *testing.T) {
		path := kimiFile(t, kimiSeed)
		f, _ := kimiFormat.Open(path)
		add := map[string][]json.RawMessage{"Stop": {kimiBlockOf(t, hookInput{Event: "Stop", Command: "bark"})}}
		if err := kimiWrite(t, f, path, map[string][]int{"Stop": {0}}, add); err != nil {
			t.Fatal(err)
		}
		got := read(t, path)
		for _, want := range []string{"# said when a turn ends", `command = "my-log"`, `command = "bark"`} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in:\n%s", want, got)
			}
		}
		if strings.Contains(got, "say done") {
			t.Errorf("the dropped block is still there:\n%s", got)
		}
	})

	t.Run("a block with a table after it, dropped, leaves the table's place", func(t *testing.T) {
		path := kimiFile(t, "x = 1\n\n[[hooks]]\nevent = \"Stop\"\ncommand = \"one\"\n\n[ui]\ntheme = \"dark\"\n")
		f, _ := kimiFormat.Open(path)
		if err := kimiWrite(t, f, path, map[string][]int{"Stop": {0}}, nil); err != nil {
			t.Fatal(err)
		}
		want := "x = 1\n\n[ui]\ntheme = \"dark\"\n"
		if got := read(t, path); got != want {
			t.Errorf("file:\n got %q\nwant %q", got, want)
		}
	})

	t.Run("a new block goes where the table's last block is, whatever is around it", func(t *testing.T) {
		// A block is added after the table's last block, and nowhere else: not
		// at the end of the file, so the user's tables after their hooks stay
		// after them. The old block is dropped and the new one added in one
		// write, as an edit of a hook is, for the first block of a file too,
		// which must not leave a blank first line behind.
		newBlock := kimiHookBlock("Stop", "", "new", 0)
		add := map[string][]json.RawMessage{"Stop": {kimiBlockOf(t, hookInput{Event: "Stop", Command: "new"})}}
		for _, c := range []struct{ name, before, want string }{
			{"a table, then the block",
				"[ui]\ntheme = \"dark\"\n\n[[hooks]]\nevent = \"Stop\"\ncommand = \"old\"\n",
				"[ui]\ntheme = \"dark\"\n\n" + newBlock},
			{"the block, then a table",
				"[[hooks]]\nevent = \"Stop\"\ncommand = \"old\"\n\n[ui]\ntheme = \"dark\"\n",
				newBlock + "\n[ui]\ntheme = \"dark\"\n"},
			{"the block alone",
				"[[hooks]]\nevent = \"Stop\"\ncommand = \"old\"\n",
				newBlock},
			{"the block, then a blank line, then a table",
				"[[hooks]]\nevent = \"Stop\"\ncommand = \"old\"\n\n[ui]\ntheme = \"dark\"\n",
				newBlock + "\n[ui]\ntheme = \"dark\"\n"},
		} {
			t.Run(c.name+", dropped and added", func(t *testing.T) {
				path := kimiFile(t, c.before)
				f, _ := kimiFormat.Open(path)
				if err := kimiWrite(t, f, path, map[string][]int{"Stop": {0}}, add); err != nil {
					t.Fatal(err)
				}
				if got := read(t, path); got != c.want {
					t.Errorf("file:\n got %q\nwant %q", got, c.want)
				}
			})
		}
		// added alone, after a last block that ends the file or sits before a table
		for _, c := range []struct{ name, before, want string }{
			{"the block alone", "[[hooks]]\nevent = \"Stop\"\ncommand = \"old\"\n",
				"[[hooks]]\nevent = \"Stop\"\ncommand = \"old\"\n\n" + newBlock},
			{"the block, then a table", "[[hooks]]\nevent = \"Stop\"\ncommand = \"old\"\n\n[ui]\ntheme = \"dark\"\n",
				"[[hooks]]\nevent = \"Stop\"\ncommand = \"old\"\n\n" + newBlock + "\n[ui]\ntheme = \"dark\"\n"},
		} {
			t.Run(c.name+", added", func(t *testing.T) {
				path := kimiFile(t, c.before)
				f, _ := kimiFormat.Open(path)
				if err := kimiWrite(t, f, path, map[string][]int{}, add); err != nil {
					t.Fatal(err)
				}
				if got := read(t, path); got != c.want {
					t.Errorf("file:\n got %q\nwant %q", got, c.want)
				}
			})
		}
	})

	t.Run("an index past the event's list is refused", func(t *testing.T) {
		path := kimiFile(t, kimiSeed)
		f, _ := kimiFormat.Open(path)
		for _, drop := range []map[string][]int{{"Stop": {2}}, {"Stop": {-1}}, {"PreToolUse": {0}}, {"Stop": {0, 5}}} {
			if err := kimiWrite(t, f, path, drop, nil); err == nil {
				t.Errorf("drop %v was taken", drop)
			}
		}
		if got := read(t, path); got != kimiSeed {
			t.Errorf("a refused write touched the file:\n%s", got)
		}
	})

	t.Run("a file that changed after it was read is left as it is", func(t *testing.T) {
		// the file was read, then the user changed the first Stop block and
		// saved before the write: index 0 of the list read first names a
		// block the file no longer has, so nothing is spliced
		path := kimiFile(t, kimiSeed)
		f, _ := kimiFormat.Open(path)
		saved := strings.Replace(kimiSeed, "say done", "say again", 1)
		write(t, path, saved)
		err := kimiWrite(t, f, path, map[string][]int{"Stop": {0}}, nil)
		if err == nil || !strings.Contains(err.Error(), "it changed since it was read") {
			t.Errorf("a write over a file that changed under it was taken: %v", err)
		}
		if got := read(t, path); got != saved {
			t.Errorf("a refused write touched the file:\n%s", got)
		}
		// a write with nothing to do doesn't look, and nothing is written
		if err := kimiWrite(t, f, path, map[string][]int{}, nil); err != nil {
			t.Errorf("a write with nothing to do said %v", err)
		}
	})
}

// A write that would leave the file not TOML is refused, and the file is as
// it was: the added block is validated with the rest.
func TestKimiFormatWriteRefusesAnInvalidResult(t *testing.T) {
	path := kimiFile(t, kimiSeed)
	f, _ := kimiFormat.Open(path)
	bad := jsonString("[[hooks]]\nevent = \"Stop\"\ncommand = \n")
	if err := kimiWrite(t, f, path, map[string][]int{}, map[string][]json.RawMessage{"Stop": {bad}}); err == nil {
		t.Error("a block that isn't TOML was written")
	}
	if got := read(t, path); got != kimiSeed {
		t.Errorf("the file was touched:\n%s", got)
	}
	// an entry that isn't a block at all
	f, _ = kimiFormat.Open(path)
	if err := kimiWrite(t, f, path, map[string][]int{}, map[string][]json.RawMessage{"Stop": {json.RawMessage(`{"a":1}`)}}); err == nil {
		t.Error("an entry that isn't a block was written")
	}
	if got := read(t, path); got != kimiSeed {
		t.Errorf("the file was touched:\n%s", got)
	}
}

// CRLF endings are kept: a kept block keeps its own, and a block added gets
// the file's.
func TestKimiFormatWriteKeepsCRLF(t *testing.T) {
	crlf := strings.ReplaceAll(kimiSeed, "\n", "\r\n")
	path := kimiFile(t, crlf)
	f, err := kimiFormat.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	add := map[string][]json.RawMessage{"Stop": {kimiBlockOf(t, hookInput{Event: "Stop", Command: "bark"})}}
	if err := kimiWrite(t, f, path, map[string][]int{"Stop": {0}}, add); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("mixed line endings:\n%q", got)
	}
	if !strings.Contains(got, "command = \"my-log\"\r\n") || !strings.Contains(got, "command = \"bark\"\r\n") || strings.Contains(got, "say done") {
		t.Errorf("file:\n%q", got)
	}
}

// A file with no final newline gets one before a block is added, and the
// last line the user had is kept as it was.
func TestKimiFormatWriteNoFinalNewline(t *testing.T) {
	path := kimiFile(t, strings.TrimSuffix(kimiSeed, "\n"))
	f, _ := kimiFormat.Open(path)
	add := map[string][]json.RawMessage{"Stop": {kimiBlockOf(t, hookInput{Event: "Stop", Command: "bark"})}}
	if err := kimiWrite(t, f, path, map[string][]int{}, add); err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSuffix(kimiSeed, "\n") + "\n\n[[hooks]]\nevent = \"Stop\"\ncommand = \"bark\"\n"
	if got := read(t, path); got != want {
		t.Errorf("file:\n got %q\nwant %q", got, want)
	}
}

// kimiHookBlock is the [[hooks]] block magpie writes for a hook, as the file
// has it.
func kimiHookBlock(ev, matcher, command string, timeout int) string {
	b := "[[hooks]]\nevent = \"" + ev + "\"\n"
	if matcher != "" {
		b += "matcher = \"" + matcher + "\"\n"
	}
	b += "command = \"" + command + "\"\n"
	if timeout > 0 {
		b += "timeout = " + strconv.Itoa(timeout) + "\n"
	}
	return b
}

// kimiRecorded is magpie's records of what it wrote in Kimi's file.
func kimiRecorded(t *testing.T) []HookEntry {
	t.Helper()
	a := mustLoad(t).Applied["kimi"]
	if a == nil {
		return nil
	}
	return a.Hooks
}

// A hook with a timeout, ticked for Claude Code and Kimi Code, lands in
// each one's file in its own shape: Claude's entry carries "timeout":10, and
// Kimi's block a timeout = 10. Both records name the hook.
func TestKimiHooksEachAgentsOwnShape(t *testing.T) {
	h := sandbox(t)
	path := kimiHere(t, h, kimiSeed)
	claude := filepath.Join(h, ".claude", "settings.json")
	ok(t)(SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "curl -s https://api.day.app/KEY/done", Timeout: 10,
		Agents: []string{"claude", "kimi"}}))

	if got := hooksOf(t, claude, "Stop"); len(got) != 1 || got[0] != `{"hooks":[{"command":"curl -s https://api.day.app/KEY/done","timeout":10,"type":"command"}]}` {
		t.Errorf("claude Stop: %v", got)
	}
	block := kimiHookBlock("Stop", "", "curl -s https://api.day.app/KEY/done", 10)
	if got := read(t, path); !strings.HasPrefix(got, kimiSeed) || !strings.HasSuffix(got, "\n"+block) {
		t.Errorf("kimi's file:\n%s", got)
	}
	recs := kimiRecorded(t)
	if len(recs) != 1 || recs[0].Name != "bark" || recs[0].Event != "Stop" {
		t.Errorf("records: %+v", recs)
	}
	// the record's entry is the block as the file has it
	if s, _ := blockText(recs[0].Entry); s != block {
		t.Errorf("record entry %q, the file's block %q", s, block)
	}

	// the page says where Kimi keeps its hooks and what it calls the event
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(v.HookAgents, func(a HookAgentView) bool { return a.ID == "kimi" })
	if i < 0 || v.HookAgents[i].File != path || v.HookAgents[i].No != "" {
		t.Errorf("kimi's view: %+v", v.HookAgents)
	} else if len(v.HookAgents[i].Events) != 20 || !slices.Contains(v.HookAgents[i].Events, "SessionHeartbeat") {
		t.Errorf("kimi's events: %v", v.HookAgents[i].Events)
	}
	j := slices.IndexFunc(v.Hooks, func(x HookView) bool { return x.Name == "bark" })
	if j < 0 || v.Hooks[j].Native["kimi"] != "Stop" || v.Hooks[j].Native["claude"] != "Stop" {
		t.Errorf("native events: %+v", v.Hooks)
	}

	ok(t)(RemoveHooks([]string{"bark"}))
	if got := read(t, path); got != kimiSeed {
		t.Errorf("kimi's file after removal:\n%s", got)
	}
}

// A timeout Kimi refuses is never written, never clamped. Ticked for Kimi
// through HookAgents, which takes no for an answer from no one, the sync
// refuses the entry and puts a Problem on the agent, the file is not
// written, and Kimi is still ticked: the page shows the Problem. The
// tick-time refusal (SomeHooksAgents) is another unit's.
func TestKimiHooksTimeoutOutOfReachIsRefused(t *testing.T) {
	h := sandbox(t)
	path := kimiHere(t, h, kimiSeed)
	ok(t)(SaveHook("", Hook{Name: "slow", Event: "turn-end", Command: "sleep", Timeout: 700, Agents: []string{"claude"}}))
	res, err := HookAgents("slow", []string{"claude", "kimi"})
	if err != nil {
		t.Fatal(err)
	}
	want := Problem{Agent: "kimi", What: "hook:slow", Error: "Kimi Code takes a hook's timeout from 1 to 600 seconds, not 700"}
	if err == nil && !slices.Contains(res.Problems, want) {
		t.Errorf("problems: %+v", res.Problems)
	}
	if got := read(t, path); got != kimiSeed {
		t.Errorf("kimi's file was written:\n%s", got)
	}
	if a := mustLoad(t).hook("slow").Agents; !slices.Equal(a, []string{"claude", "kimi"}) {
		t.Errorf("slow's agents: %v", a)
	}
	if recs := kimiRecorded(t); len(recs) != 0 {
		t.Errorf("recorded as written in kimi: %+v", recs)
	}
	// Claude's is written as asked, and the page says why Kimi's isn't
	if got := hooksOf(t, filepath.Join(h, ".claude", "settings.json"), "Stop"); len(got) != 1 || !strings.Contains(got[0], `"timeout":700`) {
		t.Errorf("claude Stop: %v", got)
	}
	v, err := Read(res.Problems)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(v.Hooks, func(x HookView) bool { return x.Name == "slow" })
	if i < 0 || v.Hooks[i].Problems["kimi"] != want.Error || v.Hooks[i].Problems["claude"] != "" {
		t.Errorf("slow's problems on the page: %+v", v.Hooks)
	}

	// unticked, the Problem goes with the tick
	ok(t)(HookAgents("slow", []string{"claude"}))
	v, _ = Read(nil)
	if i = slices.IndexFunc(v.Hooks, func(x HookView) bool { return x.Name == "slow" }); i < 0 || len(v.Hooks[i].Problems) != 0 {
		t.Errorf("slow's problems after unticking: %+v", v.Hooks[i].Problems)
	}
}

// Kimi Code rewrites its config.toml wholesale, so a block magpie wrote
// comes back with its keys in another order and another indent. It is still
// magpie's: a sync with nothing to do doesn't write the file, unticking it
// takes that block out and nothing else, and a real edit takes it out and
// puts the new one in once. Read(nil) doesn't list it under "In your agents".
func TestKimiHooksResavedBlockIsStillMine(t *testing.T) {
	h := sandbox(t)
	path := kimiHere(t, h, kimiSeed)
	ok(t)(SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "bark", Agents: []string{"kimi"}}))
	// magpie's block went in after the user's last one, a blank line parting
	// them. The user then adds one of their own after it, the same way.
	mine := "\n" + kimiHookBlock("Stop", "", "bark", 0)
	theirs := "\n" + kimiHookBlock("Stop", "", "say after", 0)
	if got := read(t, path); got != kimiSeed+mine {
		t.Fatalf("magpie's write:\n got %q\nwant %q", got, kimiSeed+mine)
	}
	// Kimi's tool saved the file again: magpie's block comes back with its
	// keys in another order and another indent
	resaved := "[[hooks]]\n    command = \"bark\"\n    event = \"Stop\"\n"
	saved := kimiSeed + resaved + theirs
	write(t, path, saved)

	// a sync with nothing to do: no write, the record kept
	before := stat(t, path)
	sleepPast(t, before)
	ok(t)(HookAgents("bark", []string{"kimi"}))
	if got := read(t, path); got != saved {
		t.Errorf("a sync with nothing to do rewrote the file:\n got %q\nwant %q", got, saved)
	}
	if after := stat(t, path); !after.ModTime().Equal(before.ModTime()) {
		t.Error("the file's time moved: it was written again")
	}
	if recs := kimiRecorded(t); len(recs) != 1 || recs[0].Name != "bark" {
		t.Errorf("the record went: %+v", recs)
	}
	// the page doesn't list magpie's own block as the user's
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if f := foundBy(v.FoundHooks, "kimi", "bark"); f != nil {
		t.Errorf("magpie's re-saved block is listed as found: %+v", f)
	}
	if f := foundBy(v.FoundHooks, "kimi", "say after"); f == nil {
		t.Errorf("the user's own isn't listed: %+v", v.FoundHooks)
	}

	// a real edit: the re-saved block comes out, the new one goes in once,
	// after the last block there is
	ok(t)(SaveHook("bark", Hook{Name: "bark", Event: "turn-end", Command: "bark louder", Agents: []string{"kimi"}}))
	without := kimiSeed + theirs
	if got, want := read(t, path), without+"\n"+kimiHookBlock("Stop", "", "bark louder", 0); got != want {
		t.Errorf("after the edit:\n got %q\nwant %q", got, want)
	}

	// untick: that block comes out and nothing else changes
	ok(t)(HookAgents("bark", []string{}))
	if got := read(t, path); got != without {
		t.Errorf("after unticking:\n got %q\nwant %q", got, without)
	}
	if recs := kimiRecorded(t); len(recs) != 0 {
		t.Errorf("records left: %+v", recs)
	}
}

// stat is a file's info, and sleepPast waits long enough that a write to it
// can be told from the one before by its time.
func stat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

func sleepPast(t *testing.T, fi os.FileInfo) {
	t.Helper()
	d := time.Until(fi.ModTime().Add(2 * time.Millisecond))
	if d > 0 {
		time.Sleep(d)
	}
}

// A [[hooks]] block of the user's own, in the shape magpie writes, is found
// under "In your agents", can be brought into the library and then taken
// out. One Kimi would refuse (a fifth key, an event Kimi lacks, a body
// indented under another table) is listed with Why "shape": it can be taken
// out of the file, and it can't be brought in, because writing it back would
// drop what makes it the user's.
func TestKimiHooksFoundOfTheUsersOwn(t *testing.T) {
	h := sandbox(t)
	seed := kimiSeed +
		"\n[[hooks]]\nevent = \"Stop\"\ncommand = \"say mine\"\ntimeout = 5\n" +
		"\n[[hooks]]\nevent = \"Stop\"\ncommand = \"with a fifth key\"\nenabled = false\n" +
		"\n[[hooks]]\nevent = \"Stop\"\ncommand = \"too slow\"\ntimeout = 900\n"
	path := kimiHere(t, h, seed)

	fs := foundHooks(t)
	mine := foundBy(fs, "kimi", "say mine")
	if mine == nil || mine.Hook == nil || mine.Hook.Event != "turn-end" || mine.Hook.Timeout != 5 || mine.Hook.Name != "say" {
		t.Errorf("a block in magpie's shape: %+v", mine)
	}
	fifth := foundBy(fs, "kimi", "with a fifth key")
	if fifth == nil || fifth.Hook != nil || fifth.Why != "shape" || fifth.Timeout != 0 {
		t.Errorf("a block with a fifth key: %+v", fifth)
	}
	slow := foundBy(fs, "kimi", "too slow")
	if slow == nil || slow.Hook != nil || slow.Why != "shape" || slow.Timeout != 0 {
		t.Errorf("a block with a timeout Kimi refuses: %+v", slow)
	}
	for _, cmd := range []string{"say done", "my-log"} {
		if f := foundBy(fs, "kimi", cmd); f == nil || f.Hook == nil {
			t.Errorf("the seed's %q: %+v", cmd, f)
		}
	}

	// one that can't be kept isn't brought in
	if _, err := ImportHooks([]string{fifth.Key}); err == nil || !strings.Contains(err.Error(), "it can only be removed") {
		t.Errorf("a block with a fifth key was brought in: %v", err)
	}
	// one in magpie's shape is, its entry left exactly as the file has it
	ok(t)(ImportHooks([]string{mine.Key}))
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.IndexFunc(v.Hooks, func(x HookView) bool { return x.Name == "say" }); i < 0 ||
		v.Hooks[i].Event != "turn-end" || v.Hooks[i].Timeout != 5 || !slices.Equal(v.Hooks[i].Agents, []string{"kimi"}) {
		t.Errorf("brought in: %+v", v.Hooks)
	}
	if got := read(t, path); got != seed {
		t.Errorf("bringing it in changed the file:\n%s", got)
	}
	// and it is magpie's from then on: unticked, it comes out alone
	ok(t)(HookAgents("say", []string{}))
	if got := read(t, path); strings.Contains(got, "say mine") || !strings.Contains(got, "say done") ||
		!strings.Contains(got, "with a fifth key") || !strings.Contains(got, "too slow") {
		t.Errorf("after unticking the brought-in one:\n%s", got)
	}

	// the one Kimi refuses is taken out of the file, the rest left as it is
	fs = foundHooks(t)
	fifth = foundBy(fs, "kimi", "with a fifth key")
	slow = foundBy(fs, "kimi", "too slow")
	if fifth == nil || slow == nil {
		t.Fatalf("found after: %+v", fs)
	}
	ok(t)(RemoveFoundHooks([]string{fifth.Key, slow.Key}))
	got := read(t, path)
	if strings.Contains(got, "with a fifth key") || strings.Contains(got, "too slow") {
		t.Errorf("still in the file:\n%s", got)
	}
	// the comment and the user's blocks around them are as they were
	for _, want := range []string{"# said when a turn ends", `command = "say done"`, `command = "my-log"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// A block under an event Kimi doesn't have is the user's, and stays in the
// file through every sync, verbatim: magpie didn't write it and doesn't take
// it out, even while it writes and removes blocks of its own around it.
func TestKimiHooksUnknownEventStays(t *testing.T) {
	h := sandbox(t)
	unknown := "[[hooks]]\nevent = \"OnSomethingElse\"\ncommand = \"keep me\"\n"
	seed := kimiSeed + "\n" + unknown + "\n[ui]\ntheme = \"dark\"\n"
	path := kimiHere(t, h, seed)

	ok(t)(SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "bark", Agents: []string{"kimi"}}))
	got := read(t, path)
	if !strings.Contains(got, unknown) {
		t.Errorf("the unknown event went on save:\n%s", got)
	}
	if !strings.Contains(got, `command = "bark"`) || !strings.HasSuffix(got, "[ui]\ntheme = \"dark\"\n") {
		t.Errorf("after save:\n%s", got)
	}
	// it isn't a found hook either: it isn't one Kimi runs
	if f := foundBy(foundHooks(t), "kimi", "keep me"); f != nil {
		t.Errorf("an event Kimi lacks is listed: %+v", f)
	}
	// a hook under an event of Kimi's own that Kimi lacks is saved, and said
	// why it isn't given: nothing is written for it
	before := read(t, path)
	res, err := SaveHook("", Hook{Name: "bad", Event: "kimi:OnSomethingElse", Command: "x", Agents: []string{"kimi"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(res.Problems, func(p Problem) bool {
		return p.Agent == "kimi" && p.What == "hook:bad" && strings.Contains(p.Error, "no event called OnSomethingElse")
	}) {
		t.Errorf("an event Kimi lacks: %+v", res.Problems)
	}
	if got := read(t, path); got != before {
		t.Errorf("written for an event Kimi lacks:\n got %q\nwant %q", got, before)
	}
	ok(t)(RemoveHooks([]string{"bad"}))

	ok(t)(RemoveHooks([]string{"bark"}))
	got = read(t, path)
	if got != seed {
		t.Errorf("after the syncs, the file isn't as the user had it:\n got %q\nwant %q", got, seed)
	}
}

// A config.toml that isn't TOML magpie can read is left as it is, nothing is
// written, and a Problem says so: what it holds is unknown, never nothing.
func TestKimiHooksUnreadableFileLeftAlone(t *testing.T) {
	h := sandbox(t)
	bad := "default_model = \n[[hooks]]\n"
	path := kimiHere(t, h, bad)
	res, err := SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "bark", Agents: []string{"kimi"}})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, path) != bad {
		t.Error("a config.toml magpie can't read was written over")
	}
	if !slices.ContainsFunc(res.Problems, func(p Problem) bool {
		return p.Agent == "kimi" && p.What == "hook:bark" && p.Error != ""
	}) {
		t.Errorf("problems: %+v", res.Problems)
	}
	if recs := kimiRecorded(t); len(recs) != 0 {
		t.Errorf("recorded as written: %+v", recs)
	}
	// a sync with nothing to write says nothing
	ok(t)(HookAgents("bark", []string{}))
	if read(t, path) != bad {
		t.Error("the file was touched by a sync with nothing to write")
	}
}

// The old Python kimi-cli shares Kimi Code's id and has no hooks: with
// ~/.kimi here and no ~/.kimi-code, no spec is given and the reason names
// what it is and where Kimi Code's hooks are. A hook ticked for it is said
// not given, and ~/.kimi/config.toml is not written.
func TestKimiHooksLegacyKimiCliHasNone(t *testing.T) {
	h := sandbox(t)
	legacy := filepath.Join(h, ".kimi")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	before := "default_model = \"native/a\"\n"
	write(t, filepath.Join(legacy, "config.toml"), before)

	for _, tg := range Targets() {
		if tg.Agent.ID == "kimi" {
			sp, why := hookSpecOf(tg)
			if sp != nil {
				t.Fatalf("the old kimi-cli was given a spec: %+v", sp)
			}
			if !strings.Contains(why, "the old kimi-cli in "+legacy+" has no hooks") || !strings.Contains(why, "Kimi Code has, in ~/.kimi-code") {
				t.Errorf("why: %q", why)
			}
		}
	}
	res, err := SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "bark", Agents: []string{"kimi"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(res.Problems, func(p Problem) bool {
		return p.Agent == "kimi" && p.What == "hook:bark" && strings.Contains(p.Error, "the old kimi-cli")
	}) {
		t.Errorf("problems: %+v", res.Problems)
	}
	if got := read(t, filepath.Join(legacy, "config.toml")); got != before {
		t.Errorf("the old kimi-cli's config was written:\n%s", got)
	}
	v, err := Read(res.Problems)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(v.HookAgents, func(a HookAgentView) bool { return a.ID == "kimi" })
	if i < 0 || !strings.Contains(v.HookAgents[i].No, "the old kimi-cli") || v.HookAgents[i].File != "" {
		t.Errorf("kimi's view: %+v", v.HookAgents)
	}

	// Kimi Code's folder here, the old one's beside it: Kimi Code's are written
	code := kimiHere(t, h, kimiSeed)
	for _, tg := range Targets() {
		if tg.Agent.ID == "kimi" {
			if sp, why := hookSpecOf(tg); sp == nil || why != "" || sp.path != code {
				t.Fatalf("Kimi Code's spec: %+v, %q", sp, why)
			}
		}
	}
	ok(t)(HookAgents("bark", []string{"kimi"}))
	if !strings.Contains(read(t, code), `command = "bark"`) || read(t, filepath.Join(legacy, "config.toml")) != before {
		t.Errorf("Kimi Code's file wasn't the one written:\n%s", read(t, code))
	}
}

// A session-start hook narrowed to some starts reaches Kimi in its own
// word for them: Kimi Code 2.7.2 tests SessionStart's matcher, a regular
// expression, on the session's source, "startup" or "resume" (hooks.go,
// startSpecs). A narrowing to a start Kimi never fires a SessionStart for
// is refused with why, never widened to every start.
func TestKimiHooksSessionStartNarrowed(t *testing.T) {
	h := sandbox(t)
	path := kimiHere(t, h, kimiSeed)
	save := func(old string, x Hook) map[string]string {
		t.Helper()
		res, err := SaveHook(old, x)
		if err != nil {
			t.Fatal(err)
		}
		v, err := Read(res.Problems)
		if err != nil {
			t.Fatal(err)
		}
		for _, y := range v.Hooks {
			if y.Name == x.Name {
				return y.Problems
			}
		}
		t.Fatalf("no hook %s in the view", x.Name)
		return nil
	}
	starts := func() []string {
		t.Helper()
		file, err := kimiFormat.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		list, err := file.List("SessionStart")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range list {
			d, _ := kimiFormat.Decode("SessionStart", e)
			m := d.Matcher
			if m == "" {
				m = "(every start)"
			}
			out = append(out, m)
		}
		return out
	}
	const cmd = "cat AGENTS.md"
	reinject := func(m string) Hook {
		return Hook{Name: "reinject", Event: "session-start", Matcher: m, Command: cmd, Agents: []string{"kimi"}}
	}

	if p := save("", reinject("startup")); p["kimi"] != "" {
		t.Errorf("startup: %+v", p)
	}
	if got := starts(); !slices.Equal(got, []string{"startup"}) {
		t.Errorf("SessionStart matchers: %q", got)
	}
	// one resumed: Kimi's own word for it
	if p := save("reinject", reinject("resume")); p["kimi"] != "" {
		t.Errorf("resume: %+v", p)
	}
	if got := starts(); !slices.Equal(got, []string{"resume"}) {
		t.Errorf("SessionStart matchers: %q", got)
	}
	// every start Kimi fires a SessionStart for is every start it has: no
	// matcher is written. A narrowing that names those and others besides
	// (startup|clear) is the same hook for Kimi, as it never starts by clear
	for _, m := range []string{"startup|resume", "startup|resume|clear", "startup|resume|clear|compact|fork"} {
		if p := save("reinject", reinject(m)); p["kimi"] != "" {
			t.Errorf("%s: %+v", m, p)
		}
		if got := starts(); !slices.Equal(got, []string{"(every start)"}) {
			t.Errorf("%s: SessionStart matchers %q", m, got)
		}
	}
	// one of Kimi's starts with others it never makes is that one alone: the
	// user ticked startup and not resume, so it doesn't run at resume
	if p := save("reinject", reinject("startup|clear|compact|fork")); p["kimi"] != "" {
		t.Errorf("startup|clear|compact|fork: %+v", p)
	}
	if got := starts(); !slices.Equal(got, []string{"startup"}) {
		t.Errorf("startup|clear|compact|fork: SessionStart matchers %q", got)
	}
	// a narrowing to starts Kimi alone never fires is refused with why. The
	// hook was every start and is none Kimi has, so its block comes out
	p := save("reinject", reinject("compact"))
	if !strings.Contains(p["kimi"], "it has no session start called compact") {
		t.Errorf("compact: %+v", p)
	}
	if got := starts(); len(got) != 0 {
		t.Errorf("a hook Kimi can't run was written: %q", got)
	}
	if p := save("reinject", reinject("clear|compact|fork")); !strings.Contains(p["kimi"], "no session start called clear or compact or fork") {
		t.Errorf("clear|compact|fork: %+v", p)
	}
	// the page offers Kimi's own two among its starts, and never one to
	// starts Kimi alone never fires; and says which starts it has
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(v.HookAgents, func(a HookAgentView) bool { return a.ID == "kimi" })
	if i < 0 {
		t.Fatalf("kimi isn't on the page: %+v", v.HookAgents)
	}
	av := v.HookAgents[i]
	for _, want := range []string{"startup", "resume", "startup|resume", "startup|clear"} {
		if !slices.Contains(av.Starts, want) {
			t.Errorf("kimi's starts don't offer %q: %v", want, av.Starts)
		}
	}
	for _, not := range []string{"clear", "compact", "fork", "clear|compact", "compact|fork"} {
		if slices.Contains(av.Starts, not) {
			t.Errorf("kimi's starts offer %q, which names no start Kimi fires: %v", not, av.Starts)
		}
	}
	if !slices.Equal(av.StartSources, []string{"startup", "resume"}) {
		t.Errorf("kimi's start sources: %v", av.StartSources)
	}
}
