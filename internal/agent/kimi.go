package agent

// Kimi Code, Moonshot's kimi CLI, keeps its settings in config.toml — the
// new one's (TypeScript, 2.x) in ~/.kimi-code ($KIMI_CODE_HOME), the old
// Python kimi-cli's in ~/.kimi ($KIMI_SHARE_DIR), in the same shape (see
// KimiDir): the model sessions start with as default_model, a key
// of its [models."<key>"] tables, each naming a [providers.<name>] table and
// the model to ask it for. magpie adds itself as the provider "magpie" (the
// gateway, spoken to as Kimi's own chat completions, so the thinking goes
// both ways as reasoning_content) and one model table per catalog model,
// keyed "magpie/<provider>/<model>", so the catalog joins Kimi's /model
// picker. The default the user had is stashed and put back when magpie steps
// out. Kimi refuses a config whose default_model or a model's provider is
// missing, so the provider goes in before the models and the default after,
// and out the other way round.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	satomic "sync/atomic"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/edit"
)

// kimiModelTable is the header prefix of every model table magpie writes.
var kimiModelTable = `models."` + magpieID + "/"

// kimiContext is what Kimi is told of a model whose context magpie doesn't
// know: it has to be told one, and compacts as it nears it.
const kimiContext = 128000

// kimiDegraded and kimiHeld hold the Notices writeMagpie found on its last
// run: a [thinking] table it couldn't read (the write fell back to the
// vendor/default effort instead of following the pick), and a section-wide
// [secondary_model].default_effort, which Kimi Code resolves ahead of every
// per-table default so the ones magpie writes are inert until it is taken
// out. Atomic so the Notice hook reads them without a lock.
var kimiDegraded, kimiHeld satomic.Value

func init() {
	kimiDegraded.Store("")
	kimiHeld.Store("")
}

// kimiNoticeText is one of the two above as a string, "" when unset.
func kimiNoticeText(v *satomic.Value) string { return v.Load().(string) }

// KimiDir is the folder Kimi Code keeps its config.toml in, and whether it is
// the old kimi-cli's. kimi-cli (1.50 on) hands over to the new Kimi Code,
// which reads only ~/.kimi-code: it offers to copy ~/.kimi's config over
// when it first starts, and once that's done never reads ~/.kimi again, so a
// model written there is one it never sees (#290). ~/.kimi is kimi-cli's only where
// the new one isn't: no ~/.kimi-code, nor $KIMI_CODE_HOME.
func KimiDir(home string) (dir string, legacy bool) { return kimiDir(here(home)) }

// kimiDir is KimiDir at a place: in a WSL distro its variables aren't
// read, and a stopped one's is ~/.kimi-code until it is looked at.
func kimiDir(at place) (dir string, legacy bool) {
	if d := at.getenv("KIMI_CODE_HOME"); d != "" {
		return d, false
	}
	code := at.join(".kimi-code")
	if at.isDir(code) {
		return code, false
	}
	if d := at.getenv("KIMI_SHARE_DIR"); d != "" {
		return d, true
	}
	if d := at.join(".kimi"); at.isDir(d) {
		return d, true
	}
	return code, false
}

// kimiThinkingEffort is the Agents-page pick, `[thinking]`'s effort key in
// Kimi Code's config: "" with no error when the table or key is absent,
// and the read error (an array table named thinking among them) so a
// caller can tell unreadable from unset. The one reader of the key: the
// effort field's Get and the model tables' writer both go through it, so
// what follows the pick and what the page shows can't drift apart.
func kimiThinkingEffort(path string) (string, error) {
	t, err := edit.GetTOMLTable(path, "thinking")
	if err != nil {
		return "", err
	}
	return t["effort"], nil
}

// kimiModelTables are magpie's model tables; the new Kimi Code is told of
// tool calling too, which kimi-cli has no word for and refuses. wrote is
// what magpie wrote in them last (kimiOwn), and the tables come back with
// what this write puts there. preferred is the Agents-page effort
// (kimiThinkingEffort), "" when none is picked: while wired, every
// magpie-owned default_effort is set to it (kimiDefaultEffort), so the
// subagents Kimi Code runs on the tables bind the effort the user asked
// for instead of each table's vendor default.
func kimiModelTables(path string, legacy bool, preferred string, wrote map[string]map[string]string) ([]edit.Table, map[string]map[string]string) {
	var out []edit.Table
	now := map[string]map[string]string{}
	names, _ := edit.TOMLTables(path)
	for _, m := range sharedModels("kimi") {
		name := "models." + strconv.Quote(magpieID+"/"+m.ID)
		ctx := m.Context
		if ctx <= 0 {
			ctx = kimiContext
		}
		var was map[string]string
		if slices.Contains(names, name) {
			was, _ = edit.GetTOMLTable(path, name)
		}
		kvs := []edit.KV{
			{Path: "provider", Value: magpieID},
			{Path: "model", Value: m.ID},
			{Path: "max_context_size", Value: ctx},
		}
		// the name Kimi's /model picker and status line show (kimi-cli 1.37
		// on; one before ignores it), else the model's id
		mine := ""
		if m.Name != m.ID {
			mine = m.Name
		}
		if v, by := kimiOwn(was, wrote[name], "display_name", mine); v != "" {
			kvs = append(kvs, edit.KV{Path: "display_name", Value: v})
			if by {
				setWrote(now, name, "display_name", v)
			}
		}
		var caps []string
		if len(m.Efforts) > 0 || !legacy && m.AlwaysThinks {
			caps = append(caps, strconv.Quote("thinking"))
		}
		// what Kimi Code's own provider says of a Kimi Code plan model
		// from the same list (packages/oauth managed-kimi-code.ts,
		// capabilitiesForModel): one that always thinks has no thinking
		// switch, and one that loads tools mid-conversation does with
		// Kimi Code's tool select on. video_in is left out: Kimi Code
		// uploads a video to its provider's /files and sends the ms://
		// id, which magpie's gateway has no route for (#4485)
		if !legacy && m.AlwaysThinks {
			caps = append(caps, strconv.Quote("always_thinking"))
		}
		if m.Images {
			caps = append(caps, strconv.Quote("image_in"))
		}
		if !legacy {
			caps = append(caps, strconv.Quote("tool_use"))
		}
		if !legacy && m.DynamicTools {
			caps = append(caps, strconv.Quote("dynamically_loaded_tools"))
		}
		if len(caps) > 0 {
			kvs = append(kvs, edit.KV{Path: "capabilities", Value: edit.Raw("[" + strings.Join(caps, ", ") + "]")})
		}
		if !legacy {
			// the new Kimi Code's cap on a reply (max_completion_tokens),
			// where magpie knows the model's; kimi-cli has no such key
			if was["max_output_size"] != "" && !kimiInt(was["max_output_size"]) {
				delete(was, "max_output_size")
			}
			mine := ""
			if n := maxTokens(m); n > 0 {
				mine = strconv.Itoa(n)
			}
			if v, by := kimiOwn(was, wrote[name], "max_output_size", mine); v != "" {
				kvs = append(kvs, edit.KV{Path: "max_output_size", Value: edit.Raw(v)})
				if by {
					setWrote(now, name, "max_output_size", v)
				}
			}
			// a default effort set by hand stays while the model has it;
			// while an Agents-page effort is picked and the agent is
			// wired, magpie's own follows it (kimiDefaultEffort)
			effs, def := kimiEfforts(m.Efforts, kimiDefaultEffort(preferred, m.DefaultEffort, m.Efforts), m.DefaultEffort, was["default_effort"], wrote[name])
			kvs = append(kvs, effs...)
			if def != "" {
				setWrote(now, name, "default_effort", def)
			}
		}
		out = append(out, edit.Table{Name: name, KVs: kvs})
	}
	return out, now
}

// kimiOwn is the value of key in one of magpie's model tables: one the user
// set there themselves (was, other than what magpie wrote last) stays,
// else magpie's own (mine, "" for none), and by says it is magpie's.
func kimiOwn(was, wrote map[string]string, key, mine string) (v string, by bool) {
	if v, ok := was[key]; ok && v != "" && v != wrote[key] {
		return v, false
	}
	return mine, mine != ""
}

// kimiInt reports whether v, a value as edit.GetTOMLTable reads it, is a
// TOML integer, which is all Kimi Code takes for a size.
func kimiInt(v string) bool {
	_, err := strconv.Atoi(strings.ReplaceAll(v, "_", ""))
	return err == nil
}

func setWrote(m map[string]map[string]string, table, key, v string) {
	if m[table] == nil {
		m[table] = map[string]string{}
	}
	m[table][key] = v
}

// kimiWroteKey is where the stash keeps what magpie last wrote in its model
// tables of the config at path, as JSON: {table: {key: value}}.
func kimiWroteKey(path string) string { return "kimi:" + path + ":wrote" }

func kimiWrote(path string) map[string]map[string]string {
	out := map[string]map[string]string{}
	if v := stashShared()[kimiWroteKey(path)]; v != "" {
		json.Unmarshal([]byte(v), &out)
	}
	return out
}

// kimiDefaultEffort is the default_effort magpie writes in one of its
// model tables: the whole selection rule, pure. want is the Agents-page
// pick: offered by the model as it stands, else the model's highest level
// (kimiHighestEffort), so a pick below a model's floor still reaches its
// strongest; a want nothing ranks (a list of words EffortRank doesn't
// know) falls through to the vendor/high chain below rather than dropping
// a key today's rule would write. With no want (""), the vendor default
// where the model offers it, else high, else no key. "none" is never a
// level: Kimi Code's own off turns thinking off.
func kimiDefaultEffort(want, vendor string, efforts []string) string {
	if want != "" && want != "none" {
		if slices.Contains(efforts, want) {
			return want
		}
		if highest := kimiHighestEffort(efforts); highest != "" {
			return highest
		}
	}
	if slices.Contains(efforts, vendor) {
		return vendor
	}
	if slices.Contains(efforts, "high") {
		return "high"
	}
	return ""
}

// kimiHighestEffort is levels' strongest as catalog.EffortRank orders the
// words (a vendor's own word for one, EffortAliases, ranks as the level
// it names): "" when none of them ranks. "none" is excluded: it isn't a
// thinking level but thinking off.
func kimiHighestEffort(levels []string) string {
	best, at := "", -1
	for _, l := range levels {
		if l == "none" {
			continue
		}
		i := slices.Index(catalog.EffortRank, catalog.EffortLevel(l))
		if i < 0 {
			continue
		}
		// a vendor's own word for the level (light for low) and the
		// level's own word rank the same: the model's list spells it,
		// and the first spelling stands so the answer is always one of
		// the model's own words, not a word only its alias knows
		if best == "" || at < i {
			best, at = l, i
		}
	}
	return best
}

// kimiEfforts are a thinking model's levels for the new Kimi Code's
// thinking picker (#333): support_efforts, and default_effort, as Kimi
// Code takes for its own models (else it starts on the middle one). The
// default is prefer, the pick kimiDefaultEffort resolved for this table
// (the Agents-page effort while one is set, else the vendor default, else
// high). Without them it offers thinking on or off only and asks for no
// level at all. none is left out: Kimi Code's own off turns thinking off.
// kimi-cli has neither key. was is the table's default_effort now and
// wrote what magpie wrote in the table last (kimiOwn): one set by hand
// stays while the model has it — except in a table magpie kept no record
// of, where a was of high or of the model's vendor default is one magpie
// wrote before it kept records (high on every model that had it, the
// vendor default where the model offered one) and is re-owned; any other
// value there is the user's hand-set. def is magpie's own default, ""
// when the table keeps the user's. A prefer outside the model's levels —
// a direct caller's; kimiDefaultEffort never makes one — writes no
// default of its own: an emitted default_effort always sits inside its
// table's support_efforts, so Kimi Code's start-up validation takes the
// config.
func kimiEfforts(efforts []string, prefer, vendor, was string, wrote map[string]string) (kvs []edit.KV, def string) {
	var levels, quoted []string
	for _, e := range efforts {
		if e != "none" {
			levels = append(levels, e)
			quoted = append(quoted, strconv.Quote(e))
		}
	}
	if len(levels) == 0 {
		return nil, ""
	}
	kvs = []edit.KV{{Path: "support_efforts", Value: edit.Raw("[" + strings.Join(quoted, ", ") + "]")}}
	mine := prefer
	if !slices.Contains(levels, mine) {
		mine = ""
		if slices.Contains(levels, "high") {
			mine = "high"
		}
	}
	// a default in a table magpie kept no record of is one it wrote
	// before it kept one: high (it wrote high on every model that had
	// it) or the model's vendor default (what it wrote where the model
	// offered one). Anything else there is the user's hand-set and stays
	if _, ok := wrote["default_effort"]; !ok && (was == "high" || vendor != "" && was == vendor) {
		was = ""
	}
	v, by := kimiOwn(map[string]string{"default_effort": was}, wrote, "default_effort", mine)
	if !by && !slices.Contains(levels, v) {
		v, by = mine, mine != ""
	}
	if v == "" {
		return kvs, ""
	}
	kvs = append(kvs, edit.KV{Path: "default_effort", Value: v})
	if by {
		def = v
	}
	return kvs, def
}

func kimi(home string) *Agent { return kimiIn(here(home)) }

// kimiProvider writes magpie's keys into its provider table, each in place
// when the table is there, so the rest of it stays: custom_headers, env or a
// default_model the user set on it (Kimi Code's ProviderConfig,
// packages/agent-core-v2/src/app/kosongConfig/configSection.ts), which a
// table written afresh dropped on every sync (#2030).
func kimiProvider(path, table string, kvs ...edit.KV) error {
	if t, err := edit.GetTOMLTable(path, table); err != nil || t == nil {
		return edit.SetTOMLTable(path, table, kvs...)
	}
	for _, kv := range kvs {
		if err := edit.SetTOMLKey(path, table, kv.Path, kv.Value); err != nil {
			return err
		}
	}
	return nil
}

// kimiIn is Kimi Code at a place: this machine's home, or a WSL distro's
// (see wsl.go), its provider naming the gateway as it reaches it from there.
func kimiIn(at place) *Agent {
	dir, legacy := kimiDir(at)
	path := filepath.Join(dir, "config.toml")
	key := "kimi:" + path + ":default_model"
	get := func() string { v, _ := edit.GetTOMLTop(path, "default_model"); return v }
	providerTable := "providers." + magpieID
	// writeMagpie takes the agent over. While wired, every magpie-owned
	// default_effort follows the Agents-page effort, kimiThinkingEffort's
	// one read of it: the tables' writer and the effort field read the
	// same key through it. A [thinking] that can't be read as a table
	// (an array table, valid TOML that syncs fine otherwise) degrades to
	// the pre-change write, preferred = "" — unknown is not unset, and
	// aborting would strip magpie's models from a config that works
	// today — and says so in a Notice. The [secondary_model] table's
	// section-wide default_effort, which Kimi Code resolves ahead of
	// every per-table default, is never written: while one is set the
	// per-table defaults magpie writes are inert, and the Notice names
	// it so the user knows.
	writeMagpie := func() error {
		preferred := ""
		degraded := ""
		if p, err := kimiThinkingEffort(path); err != nil {
			degraded = noticeKimiThinkingUnreadable.say("error", err.Error())
		} else {
			preferred = p
		}
		kimiDegraded.Store(degraded)
		if err := kimiProvider(path, providerTable,
			edit.KV{Path: "type", Value: "kimi"},
			edit.KV{Path: "base_url", Value: at.v1()},
			edit.KV{Path: "api_key", Value: at.gwKey()},
		); err != nil {
			return err
		}
		if sec, err := edit.GetTOMLTable(path, "secondary_model"); err == nil && sec["default_effort"] != "" {
			kimiHeld.Store(noticeKimiSectionEffort.say("effort", sec["default_effort"]))
		} else {
			kimiHeld.Store("")
		}
		tables, wrote := kimiModelTables(path, legacy, preferred, kimiWrote(path))
		if err := edit.SetTOMLTables(path, []string{kimiModelTable}, tables); err != nil {
			return err
		}
		// what magpie wrote goes in the stash as part of the same atomic
		// edit: a record lost after a successful write would make the
		// next sync read magpie's own value as hand-set and stop following
		b, err := json.Marshal(wrote)
		if err != nil {
			return err
		}
		stashMu.Lock()
		defer stashMu.Unlock()
		m := stashLoad()
		m[kimiWroteKey(path)] = string(b)
		return stashWrite(m)
	}
	dropMagpie := func() error {
		if err := edit.SetTOMLTables(path, []string{kimiModelTable}, nil); err != nil {
			return err
		}
		forget(kimiWroteKey(path))
		kimiDegraded.Store("")
		kimiHeld.Store("")
		return edit.DelTOMLTable(path, providerTable)
	}
	// setDefault sets default_model, or takes it out for ""
	setDefault := func(v string) error {
		if v == "" {
			return edit.DelTOMLTop(path, "default_model")
		}
		return edit.SetTOMLTop(path, edit.KV{Path: "default_model", Value: v})
	}
	// ownModel reports whether Kimi has a model of the user's by this key
	ownModel := func(k string) bool {
		t, err := kimiModelTableForKey(path, k)
		return err == nil && t != nil
	}
	a := &Agent{
		ID: "kimi", Name: "Kimi Code", Icon: "kimi", Aliases: []string{"kimi-code", "kimi-cli"}, Spelled: prefixed,
		// kimi-cli (Python) says KimiCLI/…; Kimi Code 2.x, kimi-code-cli/…
		// (jiabubu on Discord: its requests showed the raw name)
		UA:  []string{"kimicli", "kimi-code-cli"},
		Bin: "kimi", Dir: dir, Path: path,
		ListOnly: true,
		Sync: func() error {
			t, err := edit.GetTOMLTable(path, providerTable)
			if err != nil || t == nil {
				return err
			}
			return writeMagpie()
		},
		Notice: func() string {
			var advice []string
			// what writeMagpie couldn't read or must not fight with:
			// one of several sentences, joined as notices are
			if d := kimiNoticeText(&kimiDegraded); d != "" {
				advice = append(advice, d)
			}
			if h := kimiNoticeText(&kimiHeld); h != "" {
				advice = append(advice, h)
			}
			if Running(`(^|/)kimi( |$)`, `(^|/)kimi-cli( |$)`) {
				advice = append(advice, kimiNotice(legacy).String())
			}
			return strings.Join(advice, " ")
		},
		Check: func() string {
			v := get()
			if !usesMagpie(v) {
				return ""
			}
			m, err := kimiModelTableForKey(path, v)
			if err != nil {
				return err.Error()
			}
			if m == nil {
				return "Kimi Code's [models." + strconv.Quote(v) + "] (config.toml) is gone, so it no longer reaches magpie"
			}
			t, err := edit.GetTOMLTable(path, providerTable)
			if err != nil {
				return err.Error()
			}
			if t == nil {
				return "Kimi Code's [" + providerTable + "] (config.toml) is gone, so it no longer reaches magpie"
			}
			return wiringOff("Kimi Code", path, func(k string) (string, bool) { v, ok := t[k]; return v, ok },
				"base_url", at.v1(), "api_key", at.gwKey())
		},
		Fields: []Field{{
			Key: "model", Label: "model",
			Get: get,
			Set: func(v string) error {
				cur := get()
				if ref, ok := strings.CutPrefix(v, magpieID+"/"); ok && isMagpie(ref) {
					if !usesMagpie(cur) {
						stash(map[string]string{key: cur})
					}
					if err := writeMagpie(); err != nil {
						return err
					}
					return setDefault(v)
				}
				if v == "" && usesMagpie(cur) {
					// back to the default the user had, if Kimi still has it
					if was := unstash(key); was != "" && !usesMagpie(was) && ownModel(was) {
						v = was
					}
				}
				if err := setDefault(v); err != nil {
					return err
				}
				return dropMagpie()
			},
			Options: func(cur map[string]string) []Option {
				return withVia(kimiOwnOptions(path, cur["model"]), "kimi", magpieID+"/")
			},
		}},
	}
	if !legacy && !at.cold {
		levels := []string{"low", "medium", "high", "xhigh", "max"}
		a.Fields = append(a.Fields, Field{
			Key: "effort", Label: "thinking",
			Get: func() string { v, _ := kimiThinkingEffort(path); return v },
			Set: func(v string) error {
				if v == "" {
					if err := edit.DelTOMLKey(path, "thinking", "effort"); err != nil {
						return err
					}
				} else {
					if !slices.Contains(levels, v) {
						return fmt.Errorf("%q is not one of Kimi Code's thinking levels", v)
					}
					if err := edit.SetTOMLKey(path, "thinking", "effort", v); err != nil {
						return err
					}
				}
				// while magpie's models are what the agent runs, the
				// tables' default_effort follows the pick (writeMagpie);
				// not wired, there is nothing to follow (Disconnect
				// clears the model first, so its pass over the fields
				// finds usesMagpie false here and recreates nothing)
				if !usesMagpie(get()) {
					return nil
				}
				return writeMagpie()
			},
			Options: func(map[string]string) []Option {
				return append([]Option{{Note: "default"}}, static(levels...)...)
			},
		})
	}
	return atomic(a, path)
}

// kimiOwnOptions are the models of the user's own in Kimi's config: its
// Kimi Code sign-in's and any provider they added, and the current one.
func kimiOwnOptions(path, cur string) []Option {
	tables, _ := edit.TOMLTables(path)
	seen := map[string]bool{}
	var out []Option
	for _, t := range tables {
		k, ok := kimiModelKey(t)
		if !ok {
			continue
		}
		if seen[k] || strings.HasPrefix(k, magpieID+"/") {
			continue
		}
		seen[k] = true
		m, _ := edit.GetTOMLTable(path, t)
		icon := modelIcon("", m["model"])
		if icon == "" && strings.Contains(m["provider"], "kimi") {
			icon = "kimi"
		}
		out = append(out, Option{Value: k, Icon: icon})
	}
	if cur != "" && !seen[cur] && !strings.HasPrefix(cur, magpieID+"/") {
		out = append([]Option{{Value: cur, Icon: modelIcon("", cur)}}, out...)
	}
	return group("Kimi Code", out)
}

// kimiModelKey decodes one model key, rejecting nested tables.
func kimiModelKey(table string) (string, bool) {
	k, ok := strings.CutPrefix(table, "models.")
	if !ok {
		return "", false
	}
	if u, err := strconv.Unquote(k); err == nil {
		return u, true
	}
	if len(k) >= 2 && k[0] == '\'' && k[len(k)-1] == '\'' {
		k = k[1 : len(k)-1]
		return k, !strings.Contains(k, "'")
	}
	return k, k != "" && !strings.ContainsAny(k, ".\"'")
}

func kimiModelTableForKey(path, key string) (map[string]string, error) {
	tables, err := edit.TOMLTables(path)
	if err != nil {
		return nil, err
	}
	for _, table := range tables {
		if decoded, ok := kimiModelKey(table); ok && decoded == key {
			return edit.GetTOMLTable(path, table)
		}
	}
	return nil, nil
}

// what kimi says after a change (notice.go). kimi-cli reads config.toml at
// start-up. Kimi Code 2.x watches it and reloads it, so an open session takes
// new models at once, but the model picker of its web and desktop app is
// read again only when Settings › Providers is opened again (jiabubu: a
// stale picker's pick said "is not configured in config.toml").
var (
	noticeKimi        = newNotice("Kimi Code reads its settings at start-up — restart open kimi sessions to use this.")
	noticeKimiCode    = newNotice("Kimi Code reads this as it runs; in its web or desktop app, reopen Settings › Providers or reload the page to see new models.")
	noticeWSLKimi     = newNotice("{agent} in WSL {distro} reads its settings at start-up — restart open kimi sessions to use this.")
	noticeWSLKimiCode = newNotice("{agent} in WSL {distro} reads this as it runs; in its web or desktop app, reopen Settings › Providers or reload the page to see new models.")
	// what writeMagpie reports through the Notice when it went on anyway
	noticeKimiThinkingUnreadable = newNotice("Kimi Code's [thinking] table (config.toml) couldn't be read ({error}), so each model's default effort keeps its own default rather than following the thinking pick.")
	noticeKimiSectionEffort      = newNotice("Kimi Code's [secondary_model] default_effort (config.toml, now {effort}) comes before every model's own default, so the per-model defaults magpie writes don't reach its subagents until it is taken out.")
)

// kimiNotice is the advice after a change for kimi-cli (legacy) or Kimi
// Code 2.x.
func kimiNotice(legacy bool) notice {
	if legacy {
		return noticeKimi
	}
	return noticeKimiCode
}

// kimiWSLNotice is kimiNotice for Kimi in a WSL distro.
func kimiWSLNotice(at place) notice {
	if _, legacy := kimiDir(at); legacy {
		return noticeWSLKimi
	}
	return noticeWSLKimiCode
}
