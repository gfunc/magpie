package settingsapi

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

// home gives the test a home of its own, with no settings.json yet.
func home(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	testenv.SetHome(t, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("APPDATA", filepath.Join(root, "AppData", "Roaming"))
}

// sample is a value of t that isn't was, as JSON carries it.
func sample(t reflect.Type, key string, was reflect.Value) reflect.Value {
	v := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.Bool:
		v.SetBool(!was.Bool())
	case reflect.String:
		s := "x"
		switch key {
		case "vision", "imageGen", "searcher", "searcherThen":
			s = "off" // a model is checked against the catalog
		}
		for _, c := range Choices[key] {
			if c != was.String() {
				s = c
				break
			}
		}
		v.SetString(s)
	case reflect.Int, reflect.Int64, reflect.Int32:
		v.SetInt(was.Int() + 7)
	case reflect.Float64, reflect.Float32:
		v.SetFloat(was.Float() + 1.5)
	case reflect.Pointer:
		elem := reflect.Zero(t.Elem())
		if !was.IsNil() {
			elem = was.Elem()
		}
		v.Set(reflect.New(t.Elem()))
		v.Elem().Set(sample(t.Elem(), key, elem))
	case reflect.Slice:
		el := sample(t.Elem(), key, reflect.Zero(t.Elem()))
		v.Set(reflect.Append(reflect.MakeSlice(t, 0, 1), el))
	case reflect.Map:
		v.Set(reflect.MakeMap(t))
		v.SetMapIndex(sample(t.Key(), "", reflect.Zero(t.Key())), sample(t.Elem(), "", reflect.Zero(t.Elem())))
	case reflect.Struct:
		for i := range t.NumField() {
			if f := t.Field(i); f.IsExported() {
				v.Field(i).Set(sample(f.Type, "", reflect.Zero(f.Type)))
			}
		}
	}
	return v
}

// Keys are the fields POST /api/settings saves from what it's sent, no
// more and no fewer: each field of settings.Settings is sent changed, on
// its own, and the ones the save takes are compared with Keys. A field
// added to Settings that the save takes (or carries) without Keys knowing
// fails here, and so `magpie settings` can't fall behind the window
// (#4745).
func TestKeysAreWhatTheSaveWrites(t *testing.T) {
	cur := settings.Defaults()
	curJSON, err := json.Marshal(cur)
	if err != nil {
		t.Fatal(err)
	}
	st := reflect.TypeFor[settings.Settings]()
	var written []string
	for i := range st.NumField() {
		f := st.Field(i)
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if key == "" || key == "-" {
			continue
		}
		was := reflect.ValueOf(cur).Field(i)
		val := sample(f.Type, key, was)
		b, err := json.Marshal(val.Interface())
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		var m map[string]json.RawMessage
		_ = json.Unmarshal(curJSON, &m)
		m[key] = b
		body, _ := json.Marshal(m)
		in, err := Merge(body, cur, false)
		if err != nil {
			t.Fatalf("%s sent as %s: %v", key, b, err)
		}
		if !reflect.DeepEqual(reflect.ValueOf(in).Field(i).Interface(), was.Interface()) {
			written = append(written, key)
		}
	}
	// settings.Save keeps the file's own: the page can send it, and it
	// is never written by this save (SetSessionReads writes it)
	written = slices.DeleteFunc(written, func(k string) bool { return k == "sessionReads" })
	got, want := slices.Sorted(slices.Values(written)), slices.Sorted(slices.Values(Keys))
	if !slices.Equal(got, want) {
		t.Errorf("the save writes keys Keys doesn't name, or Keys names keys it doesn't write:\n only saved: %v\n only in Keys: %v",
			minus(got, want), minus(want, got))
	}
	for _, k := range Keys {
		if _, ok := Field(k); !ok {
			t.Errorf("Keys names %q, which settings.Settings doesn't have", k)
		}
	}
	if len(Keys) != len(slices.Compact(slices.Sorted(slices.Values(Keys)))) {
		t.Error("Keys names a key twice")
	}
}

func minus(a, b []string) []string {
	var out []string
	for _, k := range a {
		if !slices.Contains(b, k) {
			out = append(out, k)
		}
	}
	return out
}

// Setting one key through Body leaves every other the user set as it
// is, and a key the save keeps when it isn't sent is set even to its
// zero value.
func TestBodySetsOneKeyOnly(t *testing.T) {
	home(t)
	s := settings.Defaults()
	s.Theme, s.NoBird, s.RedactShape, s.Currency, s.Proxy = "dark", true, true, "cny", "http://127.0.0.1:7890"
	two := 2
	s.RefusalRetries = &two
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	set := func(key, value string) settings.Settings {
		t.Helper()
		cur, err := settings.LoadChecked()
		if err != nil {
			t.Fatal(err)
		}
		body, err := Body(cur, key, json.RawMessage(value))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := Apply(body, false); err != nil {
			t.Fatalf("%s=%s: %v", key, value, err)
		}
		return settings.Load()
	}
	got := set("noBird", "false")
	if got.NoBird {
		t.Error("noBird set to false stayed on: a zero value was read as not sent")
	}
	got = set("lang", `"ja"`)
	if got.Lang != "ja" || got.Theme != "dark" || !got.RedactShape || got.Currency != "cny" ||
		got.Proxy != "http://127.0.0.1:7890" || got.RefusalRetries == nil || *got.RefusalRetries != 2 {
		t.Errorf("setting lang changed what the user set: %+v", got)
	}
}

// A value the save refuses writes nothing, whichever way it came in.
func TestRefusedValueWritesNothing(t *testing.T) {
	home(t)
	s := settings.Defaults()
	s.Theme = "dark"
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"theme":           `"neon"`,
		"lang":            `"xx"`,
		"trayUsageEvery":  `4`,
		"vision":          `"nobody/nothing"`,
		"searcher":        `"nobody/nothing"`,
		"usageAlert":      `101`,
		"updateChannel":   `"nightly"`,
		"registryBuckets": `[]`,      // every protocol bucket turned off
		"registryEffort":  `"ultra"`, // not one of the five levels
	} {
		cur := settings.Load()
		body, err := Body(cur, key, json.RawMessage(value))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := Apply(body, false); err == nil {
			t.Errorf("%s=%s was saved", key, value)
		}
		after, _ := os.ReadFile(settings.Path())
		if !bytes.Equal(before, after) {
			t.Fatalf("%s=%s was refused but settings.json changed", key, value)
		}
	}
	if _, err := Body(settings.Load(), "port", json.RawMessage(`1`)); err == nil {
		t.Error("Body took port, which has a route of its own")
	}
}

// The model registry's four settings are the Settings page's to send: a
// save that sends none of them (a page from before the section exists)
// keeps the user's own, and one that sends them empty turns the registry
// off. The buckets are a list, and a list that isn't sent is the one the
// user has — a list that is sent empty is every bucket off, which the save
// refuses.
func TestRegistrySettingsCarriedUnlessSent(t *testing.T) {
	cur := settings.Defaults()
	cur.RegistryOn = true
	cur.RegistryEffort = "high"
	cur.RegistryBuckets = []string{"magpie-openai"}
	cur.RegistrySync = true
	in, err := Merge([]byte(`{"theme":"dark"}`), cur, false)
	if err != nil {
		t.Fatal(err)
	}
	if !in.RegistryOn || in.RegistryEffort != "high" || !slices.Equal(in.RegistryBuckets, []string{"magpie-openai"}) || !in.RegistrySync {
		t.Errorf("a save that sent no registry setting changed one: %+v", in)
	}
	// an explicit null is a value sent: the switches off and the effort
	// empty, which is the model's own middle
	in, err = Merge([]byte(`{"registryOn":null,"registryEffort":null,"registryBuckets":null,"registrySync":null}`), cur, false)
	if err != nil {
		t.Fatal(err)
	}
	if in.RegistryOn || in.RegistrySync || in.RegistryEffort != "" {
		t.Errorf("a save that sent the registry's settings empty left one on: %+v", in)
	}
	// the buckets are a list, and a null is no list said: the list the
	// user has is kept (as the searchers after the one named are)
	if !slices.Equal(in.RegistryBuckets, []string{"magpie-openai"}) {
		t.Errorf("a save that sent the buckets null changed them: %v", in.RegistryBuckets)
	}
	// an empty list is a list said, and reaches the save, which refuses it
	in, err = Merge([]byte(`{"registryBuckets":[]}`), cur, false)
	if err != nil {
		t.Fatal(err)
	}
	if in.RegistryBuckets == nil || len(in.RegistryBuckets) != 0 {
		t.Errorf("an empty bucket list was carried instead of sent: %v", in.RegistryBuckets)
	}
	// a file from before the registry existed says no buckets, and reads
	// as all four
	home(t)
	if err := os.MkdirAll(settings.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.Path(), []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := settings.Load().RegistryBuckets; !slices.Equal(got, settings.RegistryBucketIDs) {
		t.Errorf("a file with no buckets reads as %v, want all four", got)
	}
}

// "Don't ask again" on the Quit question (zhujiu on Discord) holds across
// a save from a page that doesn't send quitNoAsk (the tray panel, or a
// page from before it), and Settings › General's Ask before quitting
// sending it false turns the question back on.
func TestQuitNoAskKeptUnlessSent(t *testing.T) {
	cur := settings.Defaults()
	cur.QuitNoAsk = true
	in, err := Merge([]byte(`{"theme":"dark"}`), cur, false)
	if err != nil {
		t.Fatal(err)
	}
	if !in.QuitNoAsk {
		t.Error("a save that didn't send quitNoAsk turned the Quit question back on")
	}
	in, err = Merge([]byte(`{"theme":"dark","quitNoAsk":false}`), cur, false)
	if err != nil {
		t.Fatal(err)
	}
	if in.QuitNoAsk {
		t.Error("quitNoAsk sent false left the Quit question off")
	}
}

// What a reply's model name says is replyModel (ZoeCats on Discord). The
// page sends it; a page from before it sends memberModel, which changes
// it only when that page's switch was moved, so its save keeps a vendor
// name it can't show; and `magpie settings reset replyModel` (Body over
// a file with memberModel written beside it) is the model asked for.
func TestReplyModelSaves(t *testing.T) {
	for _, c := range []struct {
		name string
		cur  string // replyModel saved now
		body string
		want string // replyModel saved, "" the model asked for
	}{
		{"page picks vendor", settings.ReplyMember, `{"replyModel":"vendor","memberModel":true}`, settings.ReplyVendor},
		{"page picks asked", settings.ReplyMember, `{"replyModel":""}`, ""},
		{"page picks member", "", `{"replyModel":"member"}`, settings.ReplyMember},
		{"old page, vendor kept", settings.ReplyVendor, `{"memberModel":false}`, settings.ReplyVendor},
		{"old page turns it on", settings.ReplyVendor, `{"memberModel":true}`, settings.ReplyMember},
		{"old page turns it off", settings.ReplyMember, `{"memberModel":false}`, ""},
		{"old page, member kept", settings.ReplyMember, `{"memberModel":true}`, settings.ReplyMember},
		{"another page's save", settings.ReplyVendor, `{"theme":"dark"}`, settings.ReplyVendor},
	} {
		t.Run(c.name, func(t *testing.T) {
			home(t)
			if err := settings.Save(settings.Settings{ReplyModel: c.cur}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Apply([]byte(c.body), false); err != nil {
				t.Fatal(err)
			}
			if got := settings.Load().ReplyModel; got != c.want {
				t.Errorf("replyModel %q, want %q", got, c.want)
			}
		})
	}
	t.Run("reset", func(t *testing.T) {
		home(t)
		if err := settings.Save(settings.Settings{ReplyModel: settings.ReplyMember}); err != nil {
			t.Fatal(err)
		}
		cur := settings.Load()
		if !cur.MemberModel {
			t.Fatal("memberModel isn't written beside replyModel member")
		}
		body, err := Body(cur, "replyModel", json.RawMessage(`""`))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := Apply(body, false); err != nil {
			t.Fatal(err)
		}
		if got := settings.Load().ReplyModel; got != "" {
			t.Errorf("reset left replyModel %q", got)
		}
	})
}
