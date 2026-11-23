package library

import (
	"path/filepath"
	"slices"
	"testing"
)

// The timeout Kimi Code refuses, and the reason it gives: the one the sync
// says as a Problem and the page says on the chip, from the same error.
const kimiSlow = "Kimi Code takes a hook's timeout from 1 to 600 seconds, not 700"

// slowHook saves a hook with a timeout Kimi refuses, ticked for Claude Code
// alone, in a sandbox where both are here. The sync's refusal (the Problem
// and the file left alone) is TestKimiHooksTimeoutOutOfReachIsRefused's.
func slowHook(t *testing.T) string {
	t.Helper()
	h := sandbox(t)
	kimiHere(t, h, kimiSeed)
	ok(t)(SaveHook("", Hook{Name: "slow", Event: "turn-end", Command: "sleep", Timeout: 700, Agents: []string{"claude"}}))
	return h
}

// hookView is the page's view of the hook called name, read through Read.
func hookView(t *testing.T, name string) HookView {
	t.Helper()
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(v.Hooks, func(x HookView) bool { return x.Name == name })
	if i < 0 {
		t.Fatalf("no hook %s in the view: %+v", name, v.Hooks)
	}
	return v.Hooks[i]
}

// The page says before the click what the sync would say after it: for a
// hook with a timeout Kimi refuses, Kimi's chip has the reason and Claude
// Code's has none. Read on its own (no tick, no sync's Problem passed in) is
// what the page reads when it opens, so that is what is asked.
func TestHooksViewBlockedSaysWhyBeforeTheTick(t *testing.T) {
	slowHook(t)
	hv := hookView(t, "slow")
	if got := hv.Blocked["kimi"]; got != kimiSlow {
		t.Errorf("Blocked[kimi] = %q, want the reason the sync gives", got)
	}
	if _, has := hv.Blocked["claude"]; has {
		t.Errorf("Blocked[claude] = %q: Claude Code takes this hook", hv.Blocked["claude"])
	}
}

// A timeout Kimi takes is not a reason: the same hook at 600 has no Blocked
// key for Kimi, and is given to it.
func TestHooksViewBlockedIsSilentForATimeoutKimiTakes(t *testing.T) {
	h := sandbox(t)
	path := kimiHere(t, h, kimiSeed)
	ok(t)(SaveHook("", Hook{Name: "quick", Event: "turn-end", Command: "sleep", Timeout: 600, Agents: []string{"claude", "kimi"}}))
	if _, has := hookView(t, "quick").Blocked["kimi"]; has {
		t.Errorf("Blocked[kimi] = %q: Kimi takes a timeout of 600", hookView(t, "quick").Blocked["kimi"])
	}
	if got, want := read(t, path), kimiSeed+"\n"+kimiHookBlock("Stop", "", "sleep", 600); got != want {
		t.Errorf("kimi's file:\n got %q\nwant %q", got, want)
	}
}

// An agent magpie writes no hooks for has no Blocked entry: it has no spec to
// refuse with, and the page greys its chip from HookAgentView.No, which says
// why. A second spelling of that on the chip would be one more to translate.
func TestHooksViewBlockedLeavesAnAgentWithNoSpecToThePage(t *testing.T) {
	slowHook(t)
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(v.HookAgents, func(a HookAgentView) bool { return a.ID == "opencode" })
	if i < 0 {
		t.Fatalf("opencode isn't on the page: %+v", v.HookAgents)
	}
	if v.HookAgents[i].No == "" {
		t.Fatalf("opencode is said to take hooks: %+v", v.HookAgents[i])
	}
	if _, has := hookView(t, "slow").Blocked["opencode"]; has {
		t.Errorf("Blocked[opencode] = %q, and HookAgentView.No already says %q", hookView(t, "slow").Blocked["opencode"], v.HookAgents[i].No)
	}
}

// The tick refuses what the page said it would: turned on for Kimi, the hook
// is not given to it and the call doesn't fail; asked for both, Claude Code
// gets it and Kimi doesn't; turned off, an agent is unset as before.
func TestSomeHooksAgentsSkipsAnAgentThatRefusesTheHook(t *testing.T) {
	h := slowHook(t)
	path := filepath.Join(h, ".kimi-code", "config.toml")
	ok(t)(SomeHooksAgents([]string{"slow"}, []string{"kimi"}, true))
	if a := mustLoad(t).hook("slow").Agents; !slices.Equal(a, []string{"claude"}) {
		t.Errorf("slow's agents after Kimi was turned on: %v", a)
	}
	if got := read(t, path); got != kimiSeed {
		t.Errorf("kimi's file was written:\n%s", got)
	}

	ok(t)(SomeHooksAgents([]string{"slow"}, []string{"kimi", "claude"}, true))
	if a := mustLoad(t).hook("slow").Agents; !slices.Equal(a, []string{"claude"}) {
		t.Errorf("slow's agents after both were turned on: %v", a)
	}

	// HookAgents asks no one, so a synced library or an older bundle can have
	// Kimi ticked (the sync then refuses it, a Problem: covered by
	// TestKimiHooksTimeoutOutOfReachIsRefused). Turning it off is not refused
	if _, err := HookAgents("slow", []string{"claude", "kimi"}); err != nil {
		t.Fatal(err)
	}
	if a := mustLoad(t).hook("slow").Agents; !slices.Equal(a, []string{"claude", "kimi"}) {
		t.Fatalf("slow's agents with Kimi ticked by HookAgents: %v", a)
	}
	ok(t)(SomeHooksAgents([]string{"slow"}, []string{"kimi"}, false))
	if a := mustLoad(t).hook("slow").Agents; !slices.Equal(a, []string{"claude"}) {
		t.Errorf("slow's agents after Kimi was turned off: %v", a)
	}
}

// The bulk bar's count ("{agent} has {n} of these hooks") is drawn from the
// same question, so a hook an agent can't be given isn't one it's said to be
// missing: SomeHooksAgents on a hook Kimi refuses, with others it takes,
// gives it the others and says nothing of the first.
func TestSomeHooksAgentsGivesAnAgentTheOnesItTakes(t *testing.T) {
	h := sandbox(t)
	kimiHere(t, h, kimiSeed)
	ok(t)(SaveHook("", Hook{Name: "slow", Event: "turn-end", Command: "sleep", Timeout: 700}))
	ok(t)(SaveHook("", Hook{Name: "bark", Event: "turn-end", Command: "bark"}))
	ok(t)(SomeHooksAgents([]string{"slow", "bark"}, []string{"kimi", "claude"}, true))
	l := mustLoad(t)
	if a := l.hook("slow").Agents; !slices.Equal(a, []string{"claude"}) {
		t.Errorf("slow's agents: %v", a)
	}
	if a := l.hook("bark").Agents; !slices.Equal(a, []string{"claude", "kimi"}) {
		t.Errorf("bark's agents: %v", a)
	}
}

// hookTakes keeps its meaning for its other callers (the found hooks, a
// plugin's hooks, a project's): whether an agent can ever take the hook,
// whatever its timeout. A hook with a timeout Kimi refuses is one Kimi can
// take in general, and is still found in its file and given through a
// plugin; the entry's refusal is asked only with a spec.
func TestHookTakesIgnoresWhatOnlyASpecCanSay(t *testing.T) {
	slow := &Hook{Name: "slow", Event: "turn-end", Command: "sleep", Timeout: 700}
	if !hookTakes(slow, "kimi") {
		t.Error("hookTakes says kimi can never take a turn-end hook")
	}
	if err := hookTakesWhy(slow, "kimi", nil); err != nil {
		t.Errorf("without a spec: %v", err)
	}
	if err := hookTakesWhy(&Hook{Name: "x", Event: "turn-end", Command: "x"}, "opencode", nil); err == nil || err.Error() != "opencode has no hooks magpie can write" {
		t.Errorf("an agent magpie writes no hooks for: %v", err)
	}
	if err := hookTakesWhy(&Hook{Name: "x", Event: "codex:Stop", Command: "x"}, "claude", nil); err == nil || err.Error() != "codex:Stop is an event of another agent's" {
		t.Errorf("another agent's own event: %v", err)
	}
	if err := hookTakesWhy(&Hook{Name: "x", Event: "session-start", Matcher: "compact", Command: "x"}, "gemini", nil); err == nil || err.Error() != "it has no session start called compact" {
		t.Errorf("a start the agent never makes: %v", err)
	}
}
