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
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	satomic "sync/atomic"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/edit"
)

// kimiModelTable is the header prefix of every model table magpie writes.
var kimiModelTable = `models."` + magpieID + "/"

// kimiContext is what Kimi is told of a model whose context magpie doesn't
// know: it has to be told one, and compacts as it nears it.
const kimiContext = 128000

// kimiHeld holds the Notice writeMagpie found on its last run: a
// section-wide [secondary_model].default_effort that is not magpie's own,
// which Kimi Code resolves ahead of every per-table default, so the
// subagents run at it and not at the pick. Atomic so the Notice hook reads
// it without a lock.
var kimiHeld satomic.Value

func init() {
	kimiHeld.Store("")
}

// kimiNoticeText is the Notice above as a string, "" when unset.
func kimiNoticeText(v *satomic.Value) string { return v.Load().(string) }

// kimiThinkingEffort is the Agents-page pick, `[thinking]` in Kimi Code's
// config, as the page shows it: "off" while it says enabled = false, which
// is Kimi Code's own off (the level left standing there is for the next
// on), else its effort key. "" with no error when the table or key is
// absent, and the read error (an array table named thinking among them) so
// a caller can tell unreadable from unset. The one reader of the pick: the
// field's Get and the subagents' effort both go through it, so what the
// subagents run at and what the page shows can't drift apart.
func kimiThinkingEffort(path string) (string, error) {
	t, err := edit.GetTOMLTable(path, "thinking")
	if err != nil {
		return "", err
	}
	if t["enabled"] == "false" {
		return "off", nil
	}
	return t["effort"], nil
}

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

// kimiModelTables are magpie's model tables; the new Kimi Code is told of
// tool calling too, which kimi-cli has no word for and refuses. wrote is
// what magpie wrote in them last (kimiOwn), and the tables come back with
// what this write puts there. Each table's default_effort is the model's
// own default (kimiDefaultEffort), or its strongest level while the pin is
// on. The pick is the lead session's own: Kimi Code resolves it ahead of
// every table's default, and a subagent never reads it (it reads
// [secondary_model].default_effort), so magpie does not copy it into the
// tables.
func kimiModelTables(path string, legacy bool, wrote map[string]map[string]string) ([]edit.Table, map[string]map[string]string) {
	var out []edit.Table
	now := map[string]map[string]string{}
	names, _ := edit.TOMLTables(path)
	for _, m := range sharedModels("kimi") {
		name := "models." + strconv.Quote(magpieID+"/"+m.ID)
		ctx := m.Context
		if ctx <= 0 {
			ctx = unknownWindow(kimiContext)
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
			// magpie's own is the model's own default (kimiDefaultEffort)
			effs, def := kimiEfforts(m.Efforts, kimiDefaultEffort(m.DefaultEffort, m.Efforts), m.DefaultEffort, was["default_effort"], wrote[name])
			kvs = append(kvs, effs...)
			if def != "" {
				setWrote(now, name, "default_effort", def)
			}
		}
		out = append(out, edit.Table{Name: name, KVs: kvs})
	}
	return out, now
}

// kimiSignInEffort is the sign-in's own plan tables (the kimi CLI's, on
// its managed provider: [models."kimi-code/…"], written from Kimi's own
// list) at the pin's level: with the pin on every one of them is at its own
// strongest level, as a model run through magpie is (the live gap: the pin
// on, K3.1 Pro's sign-in table at the vendor's high). With the pin off
// magpie writes nothing and records nothing: the tables are Kimi Code's
// own, and its next start puts the vendor's default back. A hand-set value
// is the user's, told by the same wrote-record the magpie tables use, keyed
// by its table name. wrote is what magpie wrote in them last.
func kimiSignInEffort(path string, wrote map[string]map[string]string) (kvs []edit.KV, now map[string]map[string]string) {
	if !effortPinned() {
		return nil, nil
	}
	now = map[string]map[string]string{}
	// the whole file once, arrays and all, as kimiThinkingOptions reads it
	type levels struct {
		Provider       string   `toml:"provider"`
		SupportEfforts []string `toml:"support_efforts"`
		DefaultEffort  string   `toml:"default_effort"`
	}
	var doc struct {
		Models map[string]levels `toml:"models"`
	}
	b, err := edit.Read(path)
	if err != nil || toml.Unmarshal(edit.TrimBOM(b), &doc) != nil {
		return nil, nil
	}
	for name, m := range doc.Models {
		key := name
		if strings.HasPrefix(key, magpieID+"/") || m.Provider != providerKimiManaged || len(m.SupportEfforts) == 0 {
			continue
		}
		efforts := m.SupportEfforts
		if !slices.Contains(efforts, "none") && !slices.ContainsFunc(efforts, func(e string) bool { return e != "" }) {
			continue
		}
		mine := kimiDefaultEffort(m.DefaultEffort, efforts)
		if !slices.Contains(efforts, mine) {
			continue // kimiDefaultEffort fell through to nothing here
		}
		table := "models." + strconv.Quote(key)
		was := map[string]string{"default_effort": m.DefaultEffort}
		// with no record of the table, its vendor default is the CLI's
		// own write (Kimi's list names it), not the user's hand: only a
		// value that is neither the vendor's nor recorded stays
		if _, ok := wrote[table]["default_effort"]; !ok && m.DefaultEffort != "" {
			was["default_effort"] = ""
		}
		v, by := kimiOwn(was, wrote[table], "default_effort", mine)
		if v == "" || v == m.DefaultEffort {
			// nothing to write. The value there is magpie's when kimiOwn
			// says so, and it was magpie's when a record says it: the
			// record is kept either way, since it is what tells a value
			// the user sets by hand later from the one magpie put there
			if by {
				setWrote(now, table, "default_effort", v)
			} else if r, ok := wrote[table]["default_effort"]; ok {
				setWrote(now, table, "default_effort", r)
			}
			continue
		}
		kvs = append(kvs, edit.KV{Path: table, Value: v})
		if by {
			setWrote(now, table, "default_effort", v)
		}
	}
	return kvs, now
}

// kimiSubagentEffort is the [secondary_model].default_effort magpie writes
// for Kimi Code's subagents: the Agents-page pick, brought down to a level
// every model of the pool takes (Kimi refuses a section whose effort one of
// them can't take), or with the pin on the pool's own ceiling. A value the
// user set by hand, or that Kimi Code itself wrote, stays: magpie writes
// only what it wrote last, as it does for the model tables (kimiOwn), and
// the record of it is the same one, under the section's own name.
//
// With no pool there is nothing to write: a subagent runs on the lead's
// model and effort then. now is the record of what magpie wrote, advice the
// Notice to show: a value the user set by hand keeps the subagents off the
// pick, and the user should know that.
func kimiSubagentEffort(path string, wrote map[string]map[string]string, pick string) (now map[string]map[string]string, advice string, err error) {
	sec, models, ok := kimiDoc(path)
	if !ok {
		return nil, "", nil
	}
	levels := kimiPoolLevels(sec.pool(), models)
	if levels == nil {
		// no pool: a subagent runs on the lead's model and effort, and the
		// section's effort says nothing
		return nil, "", nil
	}
	want := kimiPoolEffort(pick, levels)
	record := wrote[kimiSecondary]["default_effort"]
	if sec.Effort != "" && sec.Effort != record {
		// the user's own value, or Kimi Code's: it stands, and magpie owns
		// none of it
		if sec.Effort != want {
			advice = noticeKimiSectionEffort.say("effort", sec.Effort)
		}
		return nil, advice, nil
	}
	if want == sec.Effort {
		// what magpie would write is there. Nothing is, and the record of
		// a value the pool made magpie take out goes with it
		if want == "" {
			return nil, "", nil
		}
		// the record says the value is magpie's to change
		if record == "" {
			return nil, "", nil
		}
		return map[string]map[string]string{kimiSecondary: {"default_effort": record}}, "", nil
	}
	next := sec
	next.Effort = want
	if err := kimiSecWrite(path, sec, next); err != nil {
		return nil, "", err
	}
	if want == "" {
		// the value magpie wrote is gone: it owns none of the section
		return nil, "", nil
	}
	return map[string]map[string]string{kimiSecondary: {"default_effort": want}}, "", nil
}

// kimiPoolEffort is the level magpie writes in [secondary_model].default_effort
// for Kimi Code's subagents: with the pin on the pool's own ceiling, else
// the Agents-page pick brought down to a level every model of the pool
// takes: the pick itself where the pool takes it, else the highest level
// the pool takes that does not exceed the pick, else the lowest the pool
// takes. "" when there is nothing to write: no pick, or no level the pool
// takes.
func kimiPoolEffort(pick string, levels []string) string {
	if effortPinned() {
		return kimiHighestEffort(levels)
	}
	if pick == "" {
		return ""
	}
	if slices.Contains(levels, pick) {
		return pick
	}
	want := kimiEffortRank(pick)
	best, at := "", -1
	lowest, lowestAt := "", 0
	for _, l := range levels {
		i := kimiEffortRank(l)
		if i < 0 {
			continue
		}
		if lowest == "" || i < lowestAt {
			lowest, lowestAt = l, i
		}
		if i <= want && i > at {
			best, at = l, i
		}
	}
	if best != "" {
		return best
	}
	return lowest
}

// providerKimiManaged is the provider table the kimi CLI gives its own
// Kimi Code sign-in.
const providerKimiManaged = "managed:kimi-code"

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

// mergeWrote folds what another writer recorded into the record magpie
// keeps of the file.
func mergeWrote(wrote map[string]map[string]string, more map[string]map[string]string) {
	for t, m := range more {
		if wrote[t] == nil {
			wrote[t] = map[string]string{}
		}
		for k, v := range m {
			wrote[t][k] = v
		}
	}
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
// model tables: the whole selection rule. While the pin is on, the model's
// own strongest level (pinnedEffort); else the model's vendor default where
// the model offers it, else high where the model offers it, else no key, so
// Kimi Code starts on the middle level. The Agents-page pick is not an
// input: the lead session resolves it ahead of every table's own default,
// and a subagent reads it from [secondary_model].default_effort, which
// magpie keeps in step with it (kimiSubagentEffort).
func kimiDefaultEffort(vendor string, efforts []string) string {
	if p := pinnedEffort(efforts); p != "" {
		return p
	}
	if slices.Contains(efforts, vendor) {
		return vendor
	}
	if slices.Contains(efforts, "high") {
		return "high"
	}
	return ""
}

// kimiHighestEffort is levels' strongest as catalog.EffortRank orders it
// (a vendor's own word for a level and the level's own word rank the
// same): "" when none of them ranks. "none" is excluded: it isn't a
// thinking level but thinking off.
func kimiHighestEffort(levels []string) string {
	best, at := "", -1
	for _, l := range levels {
		if l == "none" {
			continue
		}
		i := kimiEffortRank(l)
		if i < 0 {
			continue
		}
		// the first spelling stands so the answer is always one of the
		// model's own words, not a word only its alias knows
		if best == "" || at < i {
			best, at = l, i
		}
	}
	return best
}

// kimiEffortRank is the level's place in catalog.EffortRank's order, -1
// when it names no level EffortRank knows.
func kimiEffortRank(e string) int {
	return slices.Index(catalog.EffortRank, catalog.EffortLevel(e))
}

// kimiEfforts are a thinking model's levels for the new Kimi Code's
// thinking picker (#333): support_efforts, and default_effort, as Kimi
// Code takes for its own models (else it starts on the middle one): the
// one the model's list says it thinks at (vendor, Kimi Code's
// think_efforts default_effort, max on K2.8), else high where the model
// has it. Without them it offers thinking on or off only and asks for no
// level at all. none is left out: Kimi Code's own off turns thinking off.
// kimi-cli has neither key. was is the table's default_effort now and
// wrote what magpie wrote in the table last (kimiOwn): one set by hand
// stays while the model has it. def is magpie's own default, "" when the
// table keeps the user's.
// prefer is the default kimiDefaultEffort resolved for this table: the
// model's own default, or its strongest level while the pin is on. It
// writes no default of its own: an emitted default_effort always sits
// inside its table's support_efforts, so Kimi Code's start-up validation
// takes the config.
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
	writeMagpie := func() error {
		// what magpie wrote in the tables last: the magpie tables and
		// the sign-in's own, each re-derived from the file and this
		// record
		old := kimiWrote(path)
		if err := kimiProvider(path, providerTable,
			edit.KV{Path: "type", Value: "kimi"},
			edit.KV{Path: "base_url", Value: at.v1()},
			edit.KV{Path: "api_key", Value: at.gwKey()},
		); err != nil {
			return err
		}
		tables, wrote := kimiModelTables(path, legacy, old)
		if err := edit.SetTOMLTables(path, []string{kimiModelTable}, tables); err != nil {
			return err
		}
		if !legacy {
			// a model gone from the catalog out of the subagents' first:
			// Kimi refuses a pool naming a model it doesn't have
			names := map[string]bool{}
			for _, t := range tables {
				if k, err := strconv.Unquote(strings.TrimPrefix(t.Name, "models.")); err == nil {
					names[k] = true
				}
			}
			if err := kimiPruneSecondary(path, func(k string) bool { return names[k] }, kimiSecStashed(path)); err != nil {
				return err
			}
			kvs, more := kimiSignInEffort(path, old)
			for _, kv := range kvs {
				if err := edit.SetTOMLKey(path, kv.Path, "default_effort", kv.Value); err != nil {
					return err
				}
			}
			mergeWrote(wrote, more)
			// the subagents' own level: the pick, brought down to a level
			// every model of the pool takes
			pick, perr := kimiThinkingEffort(path)
			if perr == nil || effortPinned() {
				more, advice, err := kimiSubagentEffort(path, old, pick)
				if err != nil {
					return err
				}
				mergeWrote(wrote, more)
				kimiHeld.Store(advice)
			}
		}
		b, err := json.Marshal(wrote)
		if err != nil {
			return err
		}
		// what magpie wrote goes in the stash as part of the same atomic
		// edit: a record lost after a successful write would make the
		// next sync read magpie's own value as hand-set and stop following
		stashMu.Lock()
		defer stashMu.Unlock()
		m := stashLoad()
		m[kimiWroteKey(path)] = string(b)
		return stashWrite(m)
	}
	dropMagpie := func() error {
		// what magpie wrote in the tables and for the subagents, read
		// before its record goes
		wrote := kimiWrote(path)
		if !legacy {
			// magpie's models out of the subagents' first, the model they
			// had back
			if err := kimiPruneSecondary(path, func(string) bool { return false }, kimiSecStashed(path)); err != nil {
				return err
			}
			forget(kimiSecStashKey(path))
			// the level magpie wrote for the subagents goes with its
			// models, as its model tables do; a pool model of the user's
			// own may still take one, so the pool does not take it along
			if r := wrote[kimiSecondary]["default_effort"]; r != "" {
				if sec, _, ok := kimiDoc(path); ok && sec.Effort == r {
					next := sec
					next.Effort = ""
					if err := kimiSecWrite(path, sec, next); err != nil {
						return err
					}
				}
			}
		}
		if err := edit.SetTOMLTables(path, []string{kimiModelTable}, nil); err != nil {
			return err
		}
		forget(kimiWroteKey(path))
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
	// joined: magpie's provider is in the config, whichever model the
	// sessions start on. Kimi Code's /model lists the user's own models
	// beside magpie's, so a lead of their own (kimi-code/k3.1-pro), picked
	// there or on the Agents page, leaves Kimi Code connected, as Hermes
	// (yingxianyu on X: the pick turned its switch off by itself)
	joined := func() bool {
		t, err := edit.GetTOMLTable(path, providerTable)
		return err == nil && t != nil
	}
	return atomic(&Agent{
		Telemetry: kimiTelemetry(at, path),
		ID:        "kimi", Name: "Kimi Code", Icon: "kimi", Aliases: []string{"kimi-code", "kimi-cli"}, Spelled: prefixed,
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
		Joined: joined,
		// Disconnect on a lead of the user's own takes magpie out and
		// leaves the lead; one of magpie's goes back by the model field
		Unwire: func() error {
			if !joined() || usesMagpie(get()) {
				return nil
			}
			return dropMagpie()
		},
		Notice: func() string {
			var advice []string
			// what writeMagpie must not fight with: one of several
			// sentences, joined as notices are
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
		Fields: append([]Field{{
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
				if v != "" {
					// one of the user's own: the lead alone moves, magpie's
					// models stay in /model beside it (joined)
					return setDefault(v)
				}
				// Default: Kimi Code as installed, magpie out
				was := unstash(key)
				if usesMagpie(cur) {
					// back to the default the user had, if Kimi still has it
					if was != "" && !usesMagpie(was) && ownModel(was) {
						v = was
					}
				} else if ownModel(cur) {
					// on a lead of the user's own (Disconnect too): it stays
					v = cur
				}
				if err := setDefault(v); err != nil {
					return err
				}
				return dropMagpie()
			},
			Options: func(cur map[string]string) []Option {
				return withVia(kimiOwnOptions(path, cur["model"]), "kimi", magpieID+"/")
			},
		}, kimiThinkingField(path, legacy, at.cold, func() bool { return usesMagpie(get()) }, writeMagpie)}, kimiSubagents(path, legacy, at.cold, joined, writeMagpie)...),
	}, path)
}

// kimiThinkingField is the thinking level Kimi Code starts its sessions
// at (@apple_test_src on X: every other agent had one). The new Kimi Code
// keeps it in [thinking] (packages/agent-core-v2's ThinkingConfigSchema):
// enabled = false for off, else effort, the level its own /model saves
// beside default_model (persistModelSelection, thinkingEffortToConfig);
// with neither it starts each model at the model's default_effort. A level
// the model doesn't have starts it at its default too. kimi-cli has only
// thinking on or off, its top-level default_thinking. A stopped WSL
// distro's (cold) levels are the common ones: reading its config would
// start it again (#3896).
func kimiThinkingField(path string, legacy, cold bool, wired func() bool, follow func() error) Field {
	if legacy {
		return Field{
			Key: "effort", Label: "thinking",
			Get: func() string {
				switch v, _ := edit.GetTOMLTop(path, "default_thinking"); v {
				case "true":
					return "on"
				case "false":
					return "off"
				}
				return ""
			},
			Set: func(v string) error {
				if v == "" {
					return edit.DelTOMLTop(path, "default_thinking")
				}
				return edit.SetTOMLTop(path, edit.KV{Path: "default_thinking", Value: v != "off"})
			},
			Options: func(map[string]string) []Option { return static("off", "on") },
		}
	}
	return Field{
		Key: "effort", Label: "thinking",
		Get: func() string {
			v, _ := kimiThinkingEffort(path)
			return v
		},
		Set: func(v string) error {
			// while magpie's models are what the agent runs, the tables
			// are written again (writeMagpie): each table keeps the
			// model's own default, and the subagents' own key follows
			// the pick (kimiSubagentEffort). Not wired, there is nothing
			// to write (Disconnect clears the model first, so its pass
			// over the fields finds usesMagpie false here and recreates
			// nothing)
			write := func() error {
				if !wired() {
					return nil
				}
				return follow()
			}
			switch v {
			case "":
				if err := edit.DelTOMLKey(path, "thinking", "effort"); err != nil {
					return err
				}
				if err := edit.DelTOMLKey(path, "thinking", "enabled"); err != nil {
					return err
				}
				return write()
			case "off":
				// as Kimi Code's own off: the level stays for the next on
				if err := edit.SetTOMLKey(path, "thinking", "enabled", false); err != nil {
					return err
				}
				return write()
			}
			if err := edit.SetTOMLKey(path, "thinking", "enabled", true); err != nil {
				return err
			}
			if err := edit.SetTOMLKey(path, "thinking", "effort", v); err != nil {
				return err
			}
			return write()
		},
		Options: func(cur map[string]string) []Option {
			if cold {
				return static(kimiCommonLevels...)
			}
			return kimiThinkingOptions(path, cur["model"])
		},
	}
}

// kimiCommonLevels are the levels offered for a model whose own aren't known.
var kimiCommonLevels = []string{"off", "low", "medium", "high"}

// kimiThinkingOptions are the levels Kimi Code offers for model, the key
// of a model table in its config at path: off, then its support_efforts
// (its [overrides]' when they say), Default being the model's
// default_effort; off alone for a model that thinks with no levels, and
// none for one that doesn't think. A model the config doesn't describe
// (Kimi Code's own sign-in's, from its catalog) gets the common levels.
func kimiThinkingOptions(path, model string) []Option {
	type levels struct {
		Capabilities   []string `toml:"capabilities"`
		SupportEfforts []string `toml:"support_efforts"`
		DefaultEffort  string   `toml:"default_effort"`
	}
	var doc struct {
		Models map[string]struct {
			levels
			Overrides *levels `toml:"overrides"`
		} `toml:"models"`
	}
	b, err := edit.Read(path)
	if err != nil || toml.Unmarshal(edit.TrimBOM(b), &doc) != nil {
		return static(kimiCommonLevels...)
	}
	m, ok := doc.Models[model]
	if !ok {
		return static(kimiCommonLevels...)
	}
	efforts, def, caps := m.SupportEfforts, m.DefaultEffort, m.Capabilities
	if o := m.Overrides; o != nil {
		if o.SupportEfforts != nil {
			efforts = o.SupportEfforts
		}
		if o.DefaultEffort != "" {
			def = o.DefaultEffort
		}
		if o.Capabilities != nil {
			caps = o.Capabilities
		}
	}
	if len(efforts) == 0 && !slices.Contains(caps, "thinking") && !slices.Contains(caps, "always_thinking") {
		return nil
	}
	var out []Option
	if slices.Contains(efforts, def) {
		out = append(out, Option{Value: "", Note: "Kimi Code's default for the model: " + def, Takes: def})
	}
	if !slices.Contains(caps, "always_thinking") {
		out = append(out, Option{Value: "off"})
	}
	return append(out, static(efforts...)...)
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
	noticeKimiSectionEffort = newNotice("Kimi Code's [secondary_model] default_effort (config.toml, now {effort}) was set by hand, so its subagents run at it and not at the thinking level picked here. Clear the subagent effort to let it follow the pick.")
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
