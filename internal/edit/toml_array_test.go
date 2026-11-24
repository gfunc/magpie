package edit

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTOMLArrayTableEditsOnlyNamedEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	before := `# global
default_model = "native/a"
[[providers]]
name = "native"
models = [
  "a", # keep
  "b",
]
[providers.headers]
x = "keep"
[[ 'providers' ]]
name = 'magpie'
models = ["old"]
[providers.model_overrides."old"]
context_window = 10
note = """
[[providers]]
name = "fake"
"""
# keep the next section's comment
[[providers]]
name = "last"
models = ["z"]
[ui]
theme = "dark"
`
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	block := "[[providers]]\nname = \"magpie\"\nmodels = [\"new\"]\n[providers.model_overrides.\"new\"]\ncontext_window = 100\n"
	if err := SetTOMLArrayTable(path, "providers", "name", "magpie", block); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	keep := before[:strings.Index(before, "[[ 'providers' ]]")]
	tail := before[strings.Index(before, "# keep the next"):]
	if !strings.HasPrefix(string(got), keep) || !strings.HasSuffix(string(got), tail) || strings.Contains(string(got), "context_window = 10\n") {
		t.Fatalf("neighboring data or the owned subtree was edited incorrectly:\n%s", got)
	}
	if err := DelTOMLArrayTable(path, "providers", "name", "magpie"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if !strings.HasPrefix(string(got), keep) || !strings.HasSuffix(string(got), tail) || strings.Contains(string(got), `name = "magpie"`) {
		t.Fatalf("delete changed another entry:\n%s", got)
	}
}

func TestTOMLArrayTableRejectsAmbiguityAndInvalidInput(t *testing.T) {
	for _, input := range []string{
		"[[providers]]\nname = \"magpie\"\n[[providers]]\nname = \"magpie\"\n",
		"[[providers]]\nname = \"other\"\nname = \"duplicate\"\n",
		"[[providers]]\nname = [\n",
		"providers = [{ name = \"native\" }]\n",
	} {
		t.Run(input, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			os.WriteFile(path, []byte(input), 0o600)
			if err := SetTOMLArrayTable(path, "providers", "name", "magpie", "[[providers]]\nname = \"magpie\"\n"); err == nil {
				t.Fatal("accepted unsafe input")
			}
			got, _ := os.ReadFile(path)
			if string(got) != input {
				t.Fatalf("failed edit changed the file: %s", got)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SetTOMLArrayTable(path, "providers", "name", "magpie", "[[providers]]\nname = \"foreign\"\n"); err == nil {
		t.Fatal("accepted a mismatching replacement")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid replacement created a file")
	}
}

func TestTOMLTopKeepsCommentAndSpacing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("default_model  =  'native/a'  # chosen by me\n[ui]\ntheme = 'dark'\n"), 0o600)
	if err := SetTOMLTopPreserving(path, KV{Path: "default_model", Value: "magpie/group/code"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "default_model  =  \"magpie/group/code\"  # chosen by me\n[ui]\ntheme = 'dark'\n" {
		t.Fatalf("lost formatting: %s", got)
	}
}

func TestTOMLArrayTableInterleavedChildren(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	input := "[[providers]]\nname = \"magpie\"\n[ui]\ntheme = \"dark\"\n[providers.model_overrides.\"old\"]\ncontext_window = 10\n[[providers]]\nname = \"native\"\n"
	os.WriteFile(path, []byte(input), 0o600)
	if err := SetTOMLArrayTable(path, "providers", "name", "magpie", "[[providers]]\nname = \"magpie\"\nmodels = [\"new\"]\n"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "context_window = 10") || !strings.Contains(string(got), "[ui]\ntheme = \"dark\"") || !strings.Contains(string(got), "name = \"native\"") {
		t.Fatalf("interleaved subtree: %s", got)
	}
}

// SetTOMLArrayTables replaces every matching entry in one place, keeps the
// others and what lies between them, and a second run with the same blocks
// leaves the file's bytes and time alone.
func TestTOMLArrayTablesReplaceEveryMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	before := `default_model = { provider = "custom", model_id = "own" }

[[models]]
provider = "magpie"
model_id = "old-1"

[models.pricing]
output = 1.0

[[models]]
provider = "magpie"
model_id = "old-2"

[[models]]
provider = "custom"
model_id = "own"
[models.pricing]
output = 2.0

[[models]]
provider = "magpie"
model_id = "old-3"
[[command_policy.rules]]
pattern = "rm"
`
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	blocks := []string{
		"[[models]]\nprovider = \"magpie\"\nmodel_id = \"new-1\"\n\n[models.pricing]\noutput = 3.0\n",
		"[[models]]\nprovider = \"magpie\"\nmodel_id = \"new-2\"\n",
	}
	if err := SetTOMLArrayTables(path, "models", "provider", "magpie", blocks); err != nil {
		t.Fatal(err)
	}
	want := `default_model = { provider = "custom", model_id = "own" }

[[models]]
provider = "magpie"
model_id = "new-1"

[models.pricing]
output = 3.0

[[models]]
provider = "magpie"
model_id = "new-2"

[[models]]
provider = "custom"
model_id = "own"
[models.pricing]
output = 2.0

[[command_policy.rules]]
pattern = "rm"
`
	got, _ := os.ReadFile(path)
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := SetTOMLArrayTables(path, "models", "provider", "magpie", blocks); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != want {
		t.Fatalf("a second run changed the file:\n%s", got)
	}
	if st, err := os.Stat(path); err != nil || !st.ModTime().Equal(old) {
		t.Fatalf("a second run wrote the file again: %v %v", st.ModTime(), err)
	}

	if err := SetTOMLArrayTables(path, "models", "provider", "magpie", nil); err != nil {
		t.Fatal(err)
	}
	want = `default_model = { provider = "custom", model_id = "own" }

[[models]]
provider = "custom"
model_id = "own"
[models.pricing]
output = 2.0

[[command_policy.rules]]
pattern = "rm"
`
	got, _ = os.ReadFile(path)
	if string(got) != want {
		t.Fatalf("after removal got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTOMLArrayTablesAppendAndRefuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("x = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetTOMLArrayTables(path, "models", "provider", "magpie", []string{"[[models]]\nprovider = \"magpie\"\nmodel_id = \"a\"\n"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "x = 1\n\n[[models]]\nprovider = \"magpie\"\nmodel_id = \"a\"\n" {
		t.Fatalf("append got %q", got)
	}
	for _, bad := range []string{
		"[[models]]\nprovider = \"other\"\n",
		"[[models]]\nprovider = \"magpie\"\n[[models]]\nprovider = \"magpie\"\n",
		"[[models]]\nprovider = \"magpie\"\n[ui]\nx = 1\n",
		"y = 2\n[[models]]\nprovider = \"magpie\"\n",
		"[models]\nprovider = \"magpie\"\n",
	} {
		if err := SetTOMLArrayTables(path, "models", "provider", "magpie", []string{bad}); err == nil {
			t.Errorf("block %q was taken", bad)
		}
	}
}

// tomlArrayLines are the file's lines, numbered, for a failure message.
func tomlArrayLines(path string) string {
	b, _ := os.ReadFile(path)
	var b2 strings.Builder
	for i, l := range strings.Split(string(b), "\n") {
		fmt.Fprintf(&b2, "%2d %s\n", i, l)
	}
	return b2.String()
}

// A [[hooks]] block runs from its header to the end of its last key's value,
// however that value is spelled, and takes in the comments and blank lines
// before that end. A child table is not part of it. Line indices, [From, To):
//
//	 0 theme = "dark"
//	 1
//	 2 # mine                      <- comment above a block: not in the span
//	 3 [[hooks]]                   span 0: [3, 7)
//	 4 event = "Stop" # when it ends
//	 5 # a note of mine            <- comment inside a block: in the span
//	 6 command = "say done"
//	 7 [[hooks]]                   span 1: [7, 14), no blank line before it
//	 8 event = "PreToolUse"
//	 9 matcher = "Bash"
//	10 command = """
//	11 echo one
//	12 echo two
//	13 """ # tail                  <- the value ends here
//	14
//	15 [[hooks]]                   span 2: [15, 18)
//	16 event = "PostToolUse"
//	17 command = "fmt"
//	18 [[hooks.config]]            <- child table: its own, not in span 2
//	19 verbose = true
//	20
//	21 [ui]
//	22 theme = "light"
//	23
func TestTOMLArraySpansListEveryBlockExactly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "theme = \"dark\"\n" +
		"\n" +
		"# mine\n" +
		"[[hooks]]\n" +
		"event = \"Stop\" # when it ends\n" +
		"# a note of mine\n" +
		"command = \"say done\"\n" +
		"[[hooks]]\n" +
		"event = \"PreToolUse\"\n" +
		"matcher = \"Bash\"\n" +
		"command = \"\"\"\n" +
		"echo one\n" +
		"echo two\n" +
		"\"\"\" # tail\n" +
		"\n" +
		"[[hooks]]\n" +
		"event = \"PostToolUse\"\n" +
		"command = \"fmt\"\n" +
		"[[hooks.config]]\n" +
		"verbose = true\n" +
		"\n" +
		"[ui]\n" +
		"theme = \"light\"\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := TOMLArraySpans(path, "hooks")
	if err != nil {
		t.Fatal(err)
	}
	lines := SplitLines(src)
	if len(lines) != 24 {
		t.Fatalf("fixture has %d lines, the indices below assume 24", len(lines))
	}
	want := []TOMLArraySpan{
		{From: 3, To: 7, Keys: []TOMLKey{
			{Name: "event", Value: "Stop", IsScalar: true},
			{Name: "command", Value: "say done", IsScalar: true},
		}},
		{From: 7, To: 14, Keys: []TOMLKey{
			{Name: "event", Value: "PreToolUse", IsScalar: true},
			{Name: "matcher", Value: "Bash", IsScalar: true},
			{Name: "command", Value: "echo one\necho two\n", IsScalar: true},
		}},
		{From: 15, To: 18, Keys: []TOMLKey{
			{Name: "event", Value: "PostToolUse", IsScalar: true},
			{Name: "command", Value: "fmt", IsScalar: true},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spans:\n got %#v\nwant %#v\nfile:\n%s", got, want, tomlArrayLines(path))
	}
	// Each span is exactly its block in the file's own lines.
	blocks := []string{
		"[[hooks]]\nevent = \"Stop\" # when it ends\n# a note of mine\ncommand = \"say done\"\n",
		"[[hooks]]\nevent = \"PreToolUse\"\nmatcher = \"Bash\"\ncommand = \"\"\"\necho one\necho two\n\"\"\" # tail\n",
		"[[hooks]]\nevent = \"PostToolUse\"\ncommand = \"fmt\"\n",
	}
	for i, s := range got {
		if text := strings.Join(lines[s.From:s.To], "\n") + "\n"; text != blocks[i] {
			t.Errorf("span %d's lines:\n got %q\nwant %q", i, text, blocks[i])
		}
	}
	// The comment above the first block and the child's header are outside.
	for _, outside := range []int{2, 14, 18, 19} {
		for _, s := range got {
			if outside >= s.From && outside < s.To {
				t.Errorf("line %d is inside span %v:\n%s", outside, s, tomlArrayLines(path))
			}
		}
	}
}

// A child table is a span of its own only when it is asked for by name.
func TestTOMLArraySpansChildTableByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "[[hooks]]\nevent = \"Stop\"\ncommand = \"say done\"\n[[hooks.config]]\nverbose = true\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := TOMLArraySpans(path, "hooks.config")
	if err != nil {
		t.Fatal(err)
	}
	// 0 [[hooks]]  1 event  2 command  3 [[hooks.config]]  4 verbose
	want := []TOMLArraySpan{{From: 3, To: 5, Keys: []TOMLKey{{Name: "verbose", Value: "true", IsScalar: true}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v\nfile:\n%s", got, want, tomlArrayLines(path))
	}
}

// Line indices are the same whatever the line ending, and a file that stops
// without a final newline still ends its last span at its last line.
func TestTOMLArraySpansCRLFAndNoFinalNewline(t *testing.T) {
	const lf = "theme = \"dark\"\r\n\r\n[[hooks]]\r\nevent = \"Stop\"\r\ncommand = \"say done\"\r\n\r\n[[hooks]]\r\nevent = \"Stop\"\r\ncommand = \"again\"\r\n"
	for name, src := range map[string]string{
		"crlf":          lf,
		"crlf_no_final": strings.TrimSuffix(lf, "\r\n"),
		"lf_no_final":   strings.ReplaceAll(strings.TrimSuffix(lf, "\r\n"), "\r\n", "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := TOMLArraySpans(path, "hooks")
			if err != nil {
				t.Fatal(err)
			}
			// 0 theme  1  2 [[hooks]]  3 event  4 command  5  6 [[hooks]]  7 event  8 command
			if len(got) != 2 || got[0].From != 2 || got[0].To != 5 || got[1].From != 6 || got[1].To != 9 {
				t.Fatalf("spans %+v\nfile:\n%s", got, tomlArrayLines(path))
			}
			if got[0].Keys[1].Value != "say done" || got[1].Keys[1].Value != "again" {
				t.Errorf("values carry the line ending: %+v", got)
			}
		})
	}
}

// A quoted header is the same table as a bare one.
func TestTOMLArraySpansQuotedHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "[[ 'hooks' ]]\nevent = 'Stop'\ncommand = 'say done'\n[[ \"hooks\" ]]\nevent = 'Stop'\ncommand = 'again'\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := TOMLArraySpans(path, "hooks")
	if err != nil {
		t.Fatal(err)
	}
	// 0 [[ 'hooks' ]]  1 event  2 command  3 [[ "hooks" ]]  4 event  5 command
	if len(got) != 2 || got[0].From != 0 || got[0].To != 3 || got[1].From != 3 || got[1].To != 6 {
		t.Fatalf("spans %+v\nfile:\n%s", got, tomlArrayLines(path))
	}
}

// A block that is only a header is one line, and a non-scalar key says so.
func TestTOMLArraySpansHeaderOnlyAndNonScalarKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "[[hooks]]\n[[hooks]]\nevent = \"Stop\"\nargs = [\"a\", \"b\"]\nmeta = { note = \"x\" }\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := TOMLArraySpans(path, "hooks")
	if err != nil {
		t.Fatal(err)
	}
	// 0 [[hooks]]  1 [[hooks]]  2 event  3 args  4 meta
	want := []TOMLArraySpan{
		{From: 0, To: 1},
		{From: 1, To: 5, Keys: []TOMLKey{
			{Name: "event", Value: "Stop", IsScalar: true},
			{Name: "args", IsScalar: false},
			{Name: "meta", IsScalar: false},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v\nfile:\n%s", got, want, tomlArrayLines(path))
	}
}

// A header-looking line inside a multi-line value is part of the value, not
// the start of a block. This is the case a line scan gets wrong, and why
// the spans come from the parser. Line indices, [From, To):
//
//	0 [[hooks]]
//	1 event = "Stop"
//	2 command = """
//	3 [[hooks]]          <- inside the value
//	4 event = "fake"     <- inside the value
//	5 """
//	6 [[hooks]]          span 1: [6, 9)
//	7 event = "PreToolUse"
//	8 command = "real"
//	9                    <- the final newline's empty line
func TestTOMLArraySpansIgnoresHeadersInsideValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "[[hooks]]\nevent = \"Stop\"\ncommand = \"\"\"\n[[hooks]]\nevent = \"fake\"\n\"\"\"\n[[hooks]]\nevent = \"PreToolUse\"\ncommand = \"real\"\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := TOMLArraySpans(path, "hooks")
	if err != nil {
		t.Fatal(err)
	}
	want := []TOMLArraySpan{
		{From: 0, To: 6, Keys: []TOMLKey{
			{Name: "event", Value: "Stop", IsScalar: true},
			{Name: "command", Value: "[[hooks]]\nevent = \"fake\"\n", IsScalar: true},
		}},
		{From: 6, To: 9, Keys: []TOMLKey{
			{Name: "event", Value: "PreToolUse", IsScalar: true},
			{Name: "command", Value: "real", IsScalar: true},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spans:\n got %#v\nwant %#v\nfile:\n%s", got, want, tomlArrayLines(path))
	}
}

func TestTOMLArraySpansRefusals(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		got, err := TOMLArraySpans(filepath.Join(t.TempDir(), "none", "config.toml"), "hooks")
		if err != nil || len(got) != 0 {
			t.Fatalf("missing file: %v, %v", got, err)
		}
	})
	t.Run("no such table", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		os.WriteFile(path, []byte("theme = \"dark\"\n"), 0o600)
		got, err := TOMLArraySpans(path, "hooks")
		if err != nil || len(got) != 0 {
			t.Fatalf("absent table: %v, %v", got, err)
		}
	})
	t.Run("invalid name", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		os.WriteFile(path, []byte("[[hooks]]\n"), 0o600)
		for _, name := range []string{"", "a b"} {
			if _, err := TOMLArraySpans(path, name); err == nil || !strings.Contains(err.Error(), "invalid table name") {
				t.Errorf("name %q: %v", name, err)
			}
		}
	})
	for _, tc := range []struct{ name, src, want string }{
		{"syntax", "[[hooks]]\nevent = \n", "line 2, column 9"},
		{"duplicate key", "[[hooks]]\nevent = \"Stop\"\nevent = \"Stop\"\n", "already defined"},
		{"root inline then array", "hooks = []\n[[hooks]]\nevent = \"Stop\"\n", "should be an array table"},
		{"ordinary then array", "[hooks]\nx = 1\n[[hooks]]\nevent = \"Stop\"\n", "should be an array table"},
	} {
		t.Run("invalid: "+tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := TOMLArraySpans(path, "hooks")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error holding %q, got %v", tc.want, err)
			}
		})
	}
	for _, tc := range []struct{ name, src, want string }{
		{"inline value", "hooks = []\n", "hooks is an inline value, not an array table"},
		{"quoted inline value", "\"hooks\" = []\n", "hooks is an inline value, not an array table"},
		{"ordinary table", "[hooks]\nevent = \"Stop\"\n", "hooks is not an array table"},
	} {
		t.Run("not an array table: "+tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tc.src), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := TOMLArraySpans(path, "hooks")
			if err == nil || !strings.HasSuffix(err.Error(), tc.want) {
				t.Fatalf("want an error ending %q, got %v", tc.want, err)
			}
		})
	}
}
