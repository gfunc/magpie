package library

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestKimiHooksFilePassesKimiDoctor hands what magpie wrote for Kimi Code to
// Kimi's own validator. One key it doesn't take and Kimi refuses the whole
// config.toml, models and providers with the hooks, so a test that only
// reads the file back with magpie's parser proves less than this: kimi
// doctor is the vendor's word on it. Every file the sync produces along the
// way is checked: written, narrowed, edited, a block taken out.
//
// kimi itself is run, so the test needs it on PATH and is skipped without
// it, and under -short. Kimi's home is a folder of this test's own
// ($KIMI_CODE_HOME, which its spec honours, so magpie writes there too); the
// real ~/.kimi-code is neither read nor written. (kimi 2.7.2 validates only
// the file it is handed: probes.md.)
func TestKimiHooksFilePassesKimiDoctor(t *testing.T) {
	kimi, err := exec.LookPath("kimi")
	if err != nil {
		t.Skip("kimi isn't installed: ", err)
	}
	if testing.Short() {
		t.Skip("runs the real kimi binary")
	}
	sandbox(t)
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	path := filepath.Join(home, "config.toml")
	write(t, path, kimiSeed)

	doctor := func(step string) {
		t.Helper()
		cmd := exec.Command(kimi, "doctor", "config", path)
		cmd.Env = append(os.Environ(), "KIMI_CODE_HOME="+home)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("%s: kimi doctor refused the file magpie wrote (%v):\n%s\nfile:\n%s", step, err, out, read(t, path))
		}
	}
	doctor("the user's own file")

	ok(t)(SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "curl -s https://api.day.app/KEY/done", Timeout: 10, Agents: []string{"kimi"}}))
	doctor("a hook with a timeout written")
	ok(t)(SaveHook("", Hook{Name: "fmt", Event: "after-tool", Matcher: "edit", Command: "prettier --write .", Agents: []string{"kimi"}}))
	doctor("a hook narrowed to edits written")
	ok(t)(SaveHook("bark", Hook{Name: "bark", Event: "turn-end", Command: "curl -s https://api.day.app/KEY/again", Timeout: 10, Agents: []string{"kimi"}}))
	doctor("a hook edited")
	ok(t)(SaveHook("", Hook{Name: "start", Event: "session-start", Matcher: "startup", Command: "cat AGENTS.md", Agents: []string{"kimi"}}))
	doctor("a session-start hook narrowed to startup")
	ok(t)(HookAgents("bark", []string{}))
	doctor("a hook unticked")

	// what was checked is the file magpie wrote, with each step in it
	got := read(t, path)
	for _, want := range []string{`command = "prettier --write ."`, `matcher = "Write|Edit"`, `matcher = "startup"`, `command = "say done"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in the file that was checked:\n%s", want, got)
		}
	}
	if strings.Contains(got, "api.day.app/KEY/again") || strings.Contains(got, "api.day.app/KEY/done") {
		t.Errorf("bark is still in the file after it was unticked:\n%s", got)
	}
}
