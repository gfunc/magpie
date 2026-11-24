package library

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/yetone/magpie/internal/agent"
)

// startUnnarrowed are the agents that map session-start and have no
// startSpecs row, with what is known of why. An entry is one of two kinds,
// said in so many words: the agent's session start takes no matcher, with
// the source, so a hook narrowed to some starts is refused for it and never
// widened; or it was not verified, and the row is missing because nobody
// has looked. A new agent here is a statement with its evidence, not an
// omission.
var startUnnarrowed = map[string]string{
	// its importer of Claude Code's hooks says a SessionStart matcher "is
	// not supported in Cursor, hooks will fire for all triggers"
	"cursor": "takes no matcher",
	// not verified: dsh_hooks.go records what its tool matcher matches and
	// nothing of its SessionStart's, so a narrowed start is refused for it
	// until someone has looked at what dsh does with one
	"dsh": "not verified",
}

// everyAgentHere is a sandbox with every agent magpie knows made as present
// as its own registry says it lives: its folder and its settings file from
// the registry's Dir and Path, and its CLI on PATH from its Bin. Nothing is
// kept by hand about how an agent is detected. The one thing the registry
// can't say is an env its places are read through, so $GROK_APP_HOME is set
// first.
//
// Who is made present is asked of agent.All(), never of hookAgents: the list
// under test can't be the one that says who is here, or taking an agent out
// of it would also take it out of the guard's sight. That is how Kimi Code
// would slip through, being a target from its ~/.kimi-code folder alone: in
// a sandbox without one it is in neither list (LESSONS 615).
func everyAgentHere(t *testing.T) {
	t.Helper()
	h := sandbox(t)
	// before any agent's place is asked for: the Grok App's Dir is under
	// $GROK_APP_HOME, read as the agent is asked for
	t.Setenv("GROK_APP_HOME", filepath.Join(h, "grok-app-data"))
	for _, a := range agent.All() {
		if a.WSL != "" {
			continue
		}
		if a.Dir != "" {
			if err := os.MkdirAll(a.Dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		// a Path that is a folder (some agents keep their settings in a
		// folder the registry names) is made above; only a file is written
		if a.Path != "" && a.Path != a.Dir {
			if fi, err := os.Stat(a.Path); err != nil || !fi.IsDir() {
				write(t, a.Path, "")
			}
		}
		holdCLI(t, h, a.Bin)
	}
}

// TestHookAgentsAndSpecsAgree is the guard the hand-kept gate lists can't be
// half-edited. hookAgents says who magpie gives hooks to; hookSpecFor says
// whether it knows how. An agent in one and not the other is a hook ticked
// for an agent that is never written, or a spec nothing asks for. Every
// agent is made present before it is asked about, or a missing folder would
// look like a missing agent.
func TestHookAgentsAndSpecsAgree(t *testing.T) {
	everyAgentHere(t)
	targets := Targets()

	// every agent that is a target here and has a spec is in hookAgents
	for _, tg := range targets {
		if sp, _ := hookSpecFor(tg); sp != nil && !slices.Contains(hookAgents, tg.Agent.ID) {
			t.Errorf("%s has a hooks spec, so it is written hooks, but hookAgents doesn't list it", tg.Agent.ID)
		}
	}
	// every agent in hookAgents is a target here with a spec
	for _, id := range hookAgents {
		tg := targetByID(id)
		if tg == nil {
			t.Errorf("%s is in hookAgents but isn't a target in a sandbox with every agent present, so its hooks are never written", id)
			continue
		}
		if sp, why := hookSpecFor(tg); sp == nil {
			t.Errorf("%s is in hookAgents but hookSpecFor gives no spec: %s", id, why)
		}
	}

	// an agent that maps session-start says how its start is narrowed,
	// unless it is one of the named few (startUnnarrowed). A row naming a
	// start HookStarts doesn't is a start no hook can ask for.
	for _, id := range hookAgents {
		tg := targetByID(id)
		if tg == nil {
			continue
		}
		sp, _ := hookSpecFor(tg)
		if sp == nil || sp.events["session-start"] == "" {
			continue
		}
		if ss := startSpecs[id]; ss != nil {
			if startUnnarrowed[id] != "" {
				t.Errorf("%s has a startSpecs row and is in startUnnarrowed: one of the two is stale", id)
			}
			for _, s := range ss.sources {
				if !slices.Contains(HookStarts, s) {
					t.Errorf("%s has a session start %q, which HookStarts doesn't name", id, s)
				}
			}
		} else if startUnnarrowed[id] == "" {
			t.Errorf("%s maps session-start but has no startSpecs row and isn't in startUnnarrowed: add the row, or say there why its start takes no matcher", id)
		}
	}

	// an agent's own event can be given to every agent magpie writes hooks
	// for, and to Antigravity, whose hooks it writes in a project alone
	if want := append(slices.Clone(hookAgents), "agy"); !slices.Equal(ownEventAgents, want) {
		t.Errorf("ownEventAgents is %v, want hookAgents plus agy: %v", ownEventAgents, want)
	}
}
