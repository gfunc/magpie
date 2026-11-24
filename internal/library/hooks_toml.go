package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/yetone/magpie/internal/edit"
)

// flatTOML is the hooks file of an agent that keeps them as bare array
// tables, one rule each: [[<table>]] with scalar keys, Kimi Code's
// config.toml being one. What the keys mean is the agent's own, so it hands
// in how one hook is written, how a block is read back, and when two blocks
// are one hook. Where the blocks are in the file is found here, by edit's
// TOML parser and never by a line scan: a multi-line command can hold a line
// that reads like a header.
type flatTOML struct {
	table string // "hooks"
	event string // the key that names the event
	// encode is in as one block, ending in a newline
	encode func(hookInput) (string, error)
	// decode is one block read back
	decode func(ev, block string) (decodedHook, error)
	// same is whether two blocks are one hook
	same func(a, b string) bool
}

// Open is the file at path. A missing file is an empty one. One that isn't
// TOML magpie can read is an error and is left as it is: what it holds is
// unknown, never nothing to keep.
func (f flatTOML) Open(path string) (hookFile, error) {
	spans, err := edit.TOMLArraySpans(path, f.table)
	if err != nil {
		return nil, err
	}
	// the file's own bytes, whether it has blocks or not: a block added to
	// a file that had none is written after what the user has, and a write
	// is refused if the file is no longer these
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	lines := edit.SplitLines(string(edit.TrimBOM(raw)))
	return &flatFile{flatTOML: f, path: path, spans: spans, lines: lines, raw: string(raw)}, nil
}

// flatFile is a flat TOML file as flatTOML.Open read it: where each block is,
// and the file's own bytes, so a write copies them and knows if they moved.
type flatFile struct {
	flatTOML
	path  string
	raw   string
	lines []string
	spans []edit.TOMLArraySpan
}

// unchanged is whether the file is still the bytes f was opened from, or
// why it isn't.
func (f *flatFile) unchanged() error {
	raw, err := os.ReadFile(f.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if string(raw) == f.raw {
		return nil
	}
	return errors.New("it changed since it was read: it is left as it is, and the next sync writes it")
}

// block is the block at index i of the file's blocks, as the file spells it,
// its lines from the header to the last key's, each with its own line ending.
func (f *flatFile) block(i int) string {
	s := f.spans[i]
	return strings.Join(f.lines[s.From:s.To], "\n") + "\n"
}

// List is every block whose event key is ev, in file order.
func (f *flatFile) List(ev string) ([]json.RawMessage, error) {
	var out []json.RawMessage
	for i, s := range f.spans {
		if eventOf(s.Keys, f.event) == ev {
			out = append(out, jsonString(f.block(i)))
		}
	}
	return out, nil
}

// Lists is every event of events that has blocks, in events' order: the
// file says what it holds only by each block's event key, so the agent's own
// events are the ones looked for.
func (f *flatFile) Lists(events []string) ([]string, map[string][]json.RawMessage, error) {
	lists := map[string][]json.RawMessage{}
	var evs []string
	for _, ev := range events {
		x, err := f.List(ev)
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

// eventOf is the value of a block's event key, "" for one that has no event
// key or whose event isn't a string: the user's own, in no list, and never
// rewritten.
func eventOf(keys []edit.TOMLKey, event string) string {
	for _, k := range keys {
		if k.Name == event && k.IsScalar {
			return k.Value
		}
	}
	return ""
}

// Entry is in as the agent spells it: a JSON string holding the block, so it
// sits in HookEntry.Entry beside a JSON agent's entry.
func (f flatTOML) Entry(in hookInput) (json.RawMessage, error) {
	block, err := f.encode(in)
	if err != nil {
		return nil, err
	}
	return jsonString(block), nil
}

func (f flatTOML) Decode(ev string, e json.RawMessage) (decodedHook, error) {
	block, ok := blockText(e)
	if !ok {
		return decodedHook{Commands: []string{}, Why: "shape"}, nil
	}
	return f.decode(ev, block)
}

// Same is the agent's comparison of the two blocks. An entry that isn't a
// block of this file's, which only a record corrupted on disk can be, is
// compared as it is.
func (f flatTOML) Same(a, b json.RawMessage) bool {
	x, aOK := blockText(a)
	y, bOK := blockText(b)
	if !aOK || !bOK {
		return canonical(a) == canonical(b)
	}
	return f.same(x, y)
}

// jsonString is a block as a TOML agent's entry is held.
func jsonString(block string) json.RawMessage {
	b, _ := json.Marshal(block)
	return json.RawMessage(b)
}

// blockText is the block a TOML agent's entry holds, false for one that holds
// none.
func blockText(e json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(e, &s) != nil {
		return "", false
	}
	return s, true
}

// Write takes the blocks at drop's indices out and puts add's in after the
// last block of the table, every other byte of the file left as it is: a
// block kept is copied from the text the file has, never written again from
// what it means.
//
// drop's indices are into the same read's list for that event (what List
// gave), so each is placed on the file's block from the spans Open found.
// The file is looked at once more before the splice, and one that is no
// longer the bytes it was opened from is refused whole: its indices name
// another file's blocks. A block's lines go out with the blank lines before
// it; a comment above it, or between it and the one before, is the user's
// and stays. The result is validated as TOML before anything is written.
func (f *flatFile) Write(path string, drop map[string][]int, add map[string][]json.RawMessage, _ map[string][]json.RawMessage) error {
	dropped := map[int]bool{}
	for ev, idxs := range drop {
		// the file's block each index names: the i-th of the list is the i-th
		// block whose event is ev
		placed := []int{}
		for i, s := range f.spans {
			if eventOf(s.Keys, f.event) == ev {
				placed = append(placed, i)
			}
		}
		for _, i := range idxs {
			if i < 0 || i >= len(placed) {
				return fmt.Errorf("no %s block number %d to take out, of %d", ev, i, len(placed))
			}
			dropped[placed[i]] = true
		}
	}
	var adds []string
	for _, es := range add {
		for _, e := range es {
			s, ok := blockText(e)
			if !ok {
				return fmt.Errorf("a hook to write isn't a %s block", f.table)
			}
			adds = append(adds, s)
		}
	}
	if len(dropped) == 0 && len(adds) == 0 {
		return nil
	}
	// Read straight from the disk, not edit.Read: that keeps what it read
	// while the file's time and size are the same, which a write inside one
	// clock tick leaves them.
	if err := f.unchanged(); err != nil {
		return err
	}
	// head ends at the table's last block, kept or dropped; a block added
	// goes after it
	head, tail := f.aroundLastBlock(dropped)
	for _, block := range adds {
		// the block ends in "\n": SplitLines leaves no trailing empty element
		blk := edit.SplitLines(strings.TrimRight(block, "\n"))
		if len(head) > 0 && strings.TrimSpace(head[len(head)-1]) != "" {
			head = append(head, "")
		}
		head = append(head, blk...)
	}
	if len(head) > 0 && len(tail) > 0 && strings.TrimSpace(tail[0]) != "" {
		// a blank line parts the table from what follows it
		head = append(head, "")
	}
	return edit.WriteTOMLFile(path, append(head, tail...))
}

// aroundLastBlock is the file's lines in two parts around the last block of
// the table: up to and including it, and all that follows it. A block in
// dropped is left out, with the blank lines before it; a comment above it,
// or between it and the one before, is the user's and stays. A file with no
// block is all head, its trailing blank lines taken off.
func (f *flatFile) aroundLastBlock(dropped map[int]bool) (head, tail []string) {
	if len(f.spans) == 0 {
		head = f.lines
		for len(head) > 0 && strings.TrimSpace(head[len(head)-1]) == "" {
			head = head[:len(head)-1]
		}
		return head, nil
	}
	at := 0
	for i, s := range f.spans {
		head = append(head, f.lines[at:s.From]...)
		if dropped[i] {
			for len(head) > 0 && strings.TrimSpace(head[len(head)-1]) == "" {
				head = head[:len(head)-1]
			}
		} else {
			head = append(head, f.lines[s.From:s.To]...)
		}
		at = s.To
	}
	return head, f.lines[at:]
}
