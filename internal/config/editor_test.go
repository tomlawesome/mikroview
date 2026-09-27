// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// v010Config is shaped like a config written for v0.1.0: the keys its
// example shipped live (listen.syslogUdp/syslogTcp and store.maxEvents,
// all removed in v0.2.0), in v0.1.0's order, with an operator's own
// comments -- above a removed key, beside a value, and on their own line.
const v010Config = `# My home MikroView -- set up March 2026.

listen:
  # UDP from the old RB4011; keep until the switch-over
  syslogUdp: ":1514"
  syslogTcp: ":1514"
  http: ":8080"   # published on 8080 by compose
  httpRedirect: ":8081"
store:
  retention: 24h
  # enough for a week of the quiet VLAN
  maxEvents: 200000

# the routers
devices:
  - id: core-router
    name: "Core Router"   # the one in the hall cupboard
    sourceIp: 192.168.1.1
reputation:
  abuseIPDBKey: "not-a-real-key-0000"
`

func readExample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../deploy/config.example.yaml")
	if err != nil {
		t.Fatalf("reading the example config: %v", err)
	}
	return string(raw)
}

func TestParseHeaderReadsTheExample(t *testing.T) {
	h, ok := ParseHeader(readExample(t))
	if !ok {
		t.Fatal("deploy/config.example.yaml has no header ParseHeader can read")
	}
	if h.Schema != CurrentSchema || h.Layout != CurrentLayout || !strings.HasPrefix(h.WrittenBy, "v") {
		t.Errorf("example header = %+v, want schema %d, layout %d, a vX.Y.Z written-by", h, CurrentSchema, CurrentLayout)
	}
}

func TestParseHeaderWithoutOne(t *testing.T) {
	if h, ok := ParseHeader(v010Config); ok {
		t.Errorf("a headerless file parsed as %+v", h)
	}
	// The title alone, without the schema line, is not a header.
	if _, ok := ParseHeader("# MikroView configuration -- keep these four lines\nlisten: {}\n"); ok {
		t.Error("a title with no schema line parsed as a header")
	}
}

func TestWriteHeaderPrependsAndReplaces(t *testing.T) {
	h := Header{Schema: 7, WrittenBy: "v0.6.1", Layout: 1}
	withHeader := WriteHeader(v010Config, h)
	if !strings.HasPrefix(withHeader, HeaderLines(h)+"\n\n# My home MikroView") {
		t.Fatalf("header not prepended with a blank line after it:\n%s", withHeader[:300])
	}
	got, ok := ParseHeader(withHeader)
	if !ok || got != h {
		t.Fatalf("round trip = %+v, %v, want %+v", got, ok, h)
	}

	// Writing again replaces the block where it stands rather than
	// stacking a second one on top.
	h2 := Header{Schema: 12, WrittenBy: "0.9.0", Layout: 2}
	again := WriteHeader(withHeader, h2)
	if strings.Count(again, "# MikroView configuration") != 1 {
		t.Fatalf("header written twice:\n%s", again)
	}
	if got, _ := ParseHeader(again); got != (Header{Schema: 12, WrittenBy: "v0.9.0", Layout: 2}) {
		t.Errorf("replaced header = %+v", got)
	}
	if !strings.HasSuffix(again, v010Config) {
		t.Error("replacing the header changed the rest of the file")
	}
	// The ratified block pads its notes to line up, two-digit schema
	// included.
	if !strings.Contains(again, "# schema: 12           (which settings") {
		t.Errorf("schema line not padded:\n%s", HeaderLines(h2))
	}
}

func TestGuessSchema(t *testing.T) {
	cases := []struct {
		name, text string
		want       int
	}{
		{"v0.1.0 keys only", v010Config, 1},
		{"engine.storePath is v0.3.0's", "engine:\n  storePath: /x\n", 3},
		{"newest key wins over a stale removed one", "listen:\n  syslogUdp: \":1514\"\nprefs:\n  storePath: /x\n", 7},
		{"commented keys are not evidence", "# prefs:\n#   storePath: /x\nlisten:\n  http: \":8080\"\n", 1},
		{"configDrift dates a file to v0.6.0", "configDrift:\n  storePath: /x\n", 6},
		{"unparseable", "listen: [", 1},
	}
	for _, c := range cases {
		if got := GuessSchema(c.text); got != c.want {
			t.Errorf("%s: GuessSchema = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestSchemaForVersion(t *testing.T) {
	for v, want := range map[string]int{"v0.1.0": 1, "0.3.1": 3, "v0.5.1": 5, "v0.6.1": 7} {
		if got, ok := SchemaForVersion(v); !ok || got != want {
			t.Errorf("SchemaForVersion(%q) = %d, %v, want %d", v, got, ok, want)
		}
	}
	if _, ok := SchemaForVersion("v9.9.9"); ok {
		t.Error("an unknown version resolved")
	}
	if last := schemaByVersion[len(schemaByVersion)-1].Schema; last != CurrentSchema {
		t.Errorf("the newest release in schemaByVersion reads schema %d but CurrentSchema is %d -- a key change needs both", last, CurrentSchema)
	}
}

// configPaths is every leaf and section path Config declares.
func configPaths() map[string]bool {
	out := map[string]bool{}
	for typ, prefix := range yamlPathPrefixes() {
		for i := 0; i < typ.NumField(); i++ {
			name := strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			if prefix != "" {
				name = prefix + "." + name
			}
			out[name] = true
		}
	}
	return out
}

func TestSchemaKeysExist(t *testing.T) {
	paths := configPaths()
	for schema, keys := range keysIntroducedAt {
		for _, k := range keys {
			_, removed := removedOrRenamedKeys[k]
			_, sectionRemoved := removedOrRenamedKeys[strings.Split(k, ".")[0]]
			if !paths[k] && !removed && !sectionRemoved {
				t.Errorf("keysIntroducedAt[%d] lists %q, which is neither a Config key nor in removedOrRenamedKeys", schema, k)
			}
		}
	}
}

func TestSecretKeysExist(t *testing.T) {
	paths := configPaths()
	if len(secretKeys) != 11 {
		t.Errorf("secretKeys has %d entries; #1347's facts note lists eleven -- update this count only with a reason", len(secretKeys))
	}
	for _, sk := range secretKeys {
		if !paths[sk.Key] {
			t.Errorf("secretKeys names %q, which is not a Config key -- a renamed field would leave its secret unmasked", sk.Key)
		}
	}
}

const secretsConfig = `reputation:
  abuseIPDBKey: fakefakefake   # the free tier
notify:
  smtp:
    host: mail.example.com
    password: "quoted pass # not a comment"
  pushover:
    token: 'single''quoted'
    user: uKey5678
  webhook:
    url: https://hooks.example.com/x
    headers:
      Authorization: "Bearer abc.def"
      X-Other: xyz999
oidc:
  clientId: mikroview
  clientSecret: |
    line-one-secret
    line-two-secret
  publicBaseUrl: https://mv.example.com
tls:
  keyFile: /etc/mikroview/certs/tls.key
history:
  keyFile: /etc/mikroview/keys/history.key
`

func TestMaskUnmaskRoundTrip(t *testing.T) {
	masked, values := MaskSecrets(secretsConfig)
	for _, secret := range []string{"fakefakefake", "quoted pass", "single''quoted", "uKey5678", "Bearer abc.def", "xyz999", "line-one-secret", "line-two-secret"} {
		if strings.Contains(masked, secret) {
			t.Errorf("masked text still contains %q:\n%s", secret, masked)
		}
	}
	for _, key := range []string{"reputation.abuseIPDBKey", "notify.smtp.password", "notify.pushover.token", "notify.pushover.user",
		"notify.webhook.headers.Authorization", "notify.webhook.headers.X-Other", "oidc.clientSecret"} {
		if !strings.Contains(masked, SecretPlaceholder(key)) {
			t.Errorf("masked text has no placeholder for %s", key)
		}
		if _, ok := values[key]; !ok {
			t.Errorf("no value recorded for %s", key)
		}
	}
	// Paths to secrets are not secrets, and stay readable.
	for _, path := range []string{"/etc/mikroview/certs/tls.key", "/etc/mikroview/keys/history.key"} {
		if !strings.Contains(masked, path) {
			t.Errorf("path %s was masked; paths are not secrets", path)
		}
	}
	// The comment beside a masked value survives.
	if !strings.Contains(masked, "# the free tier") {
		t.Error("a comment on a masked line was lost")
	}
	// Values are kept as written, quotes and all.
	if values["notify.smtp.password"] != `"quoted pass # not a comment"` {
		t.Errorf("smtp password recorded as %q", values["notify.smtp.password"])
	}

	if got := UnmaskSecrets(masked, values); got != secretsConfig {
		t.Fatalf("unmask did not give back the original:\n%s", got)
	}

	// An edit to a non-secret line survives the round trip, and every
	// secret comes back untouched around it.
	edited := strings.Replace(masked, "host: mail.example.com", "host: smtp.example.org", 1)
	want := strings.Replace(secretsConfig, "host: mail.example.com", "host: smtp.example.org", 1)
	if got := UnmaskSecrets(edited, values); got != want {
		t.Fatalf("an edited non-secret line did not survive:\n%s", got)
	}
	// A secret typed over its placeholder is what comes out.
	typed := strings.Replace(masked, SecretPlaceholder("notify.pushover.user"), "newUser0000", 1)
	if got := UnmaskSecrets(typed, values); !strings.Contains(got, "user: newUser0000") {
		t.Errorf("a typed secret was overwritten on unmask:\n%s", got)
	}
	// The masked text is still valid YAML that loads.
	if problems := ValidateText(masked); hasFatal(problems) {
		t.Errorf("masked text does not validate: %+v", problems)
	}
}

func TestUnmaskLeavesUnknownPlaceholders(t *testing.T) {
	text := "oidc:\n  clientSecret: " + SecretPlaceholder("oidc.clientSecret") + "\n"
	if got := UnmaskSecrets(text, map[string]string{}); got != text {
		t.Errorf("an unknown placeholder was changed: %q", got)
	}
	if keys := SecretPlaceholders(text); !reflect.DeepEqual(keys, []string{"oidc.clientSecret"}) {
		t.Errorf("SecretPlaceholders = %v", keys)
	}
}

func hasFatal(problems []TextProblem) bool {
	for _, p := range problems {
		if p.Severity == "fatal" {
			return true
		}
	}
	return false
}

func TestValidateTextPlacesProblemsOnTheirLines(t *testing.T) {
	problems := ValidateText(v010Config)
	want := map[string]int{"listen.syslogUdp": 5, "listen.syslogTcp": 6, "store.maxEvents": 12}
	for key, line := range want {
		found := false
		for _, p := range problems {
			if p.Key == key {
				found = true
				if p.Line != line || p.Severity != "fatal" || !strings.Contains(p.Message, "was removed in v0.2.0") {
					t.Errorf("%s: got %+v, want line %d, fatal, naming the removal", key, p, line)
				}
			}
		}
		if !found {
			t.Errorf("no problem reported for %s: %+v", key, problems)
		}
	}

	// A config.Validate fatal lands on its key's line too.
	p := findProblem(ValidateText("listen:\n  http: \":8080\"\nauth:\n  sessionTTL: 0s\n"), "auth.sessionTTL")
	if p == nil || p.Line != 4 || p.Severity != "fatal" {
		t.Errorf("auth.sessionTTL: got %+v, want a fatal on line 4", p)
	}

	// A syntax error names its line.
	syntax := ValidateText("listen:\n  http: \":8080\"\n bad: [\n")
	if len(syntax) != 1 || syntax[0].Line == 0 || syntax[0].Severity != "fatal" {
		t.Errorf("syntax error: %+v", syntax)
	}

	// The empty file is defaults, and valid.
	if hasFatal(ValidateText("")) {
		t.Errorf("an empty file has fatal problems: %+v", ValidateText(""))
	}
}

func findProblem(problems []TextProblem, key string) *TextProblem {
	for i := range problems {
		if problems[i].Key == key {
			return &problems[i]
		}
	}
	return nil
}

func TestCarryForwardAV010Config(t *testing.T) {
	out, changes, problems := CarryForward(v010Config, "v0.6.1")

	if hasFatal(problems) {
		t.Fatalf("carried-forward file would still be refused: %+v\n%s", problems, out)
	}
	if hasFatal(ValidateText(out)) {
		t.Fatal("ValidateText disagrees with CarryForward's own verdict")
	}
	for _, gone := range []string{"syslogUdp", "syslogTcp", "maxEvents"} {
		if strings.Contains(out, gone) {
			t.Errorf("%s survived Carry forward:\n%s", gone, out)
		}
	}
	// Every operator comment is kept, and every value.
	for _, kept := range []string{
		"# My home MikroView -- set up March 2026.",
		"# UDP from the old RB4011; keep until the switch-over",
		`http: ":8080"   # published on 8080 by compose`,
		"# enough for a week of the quiet VLAN",
		"# the routers",
		`name: "Core Router"   # the one in the hall cupboard`,
		"retention: 24h",
		`abuseIPDBKey: "not-a-real-key-0000"`,
	} {
		if !strings.Contains(out, kept) {
			t.Errorf("Carry forward lost %q:\n%s", kept, out)
		}
	}
	h, ok := ParseHeader(out)
	if !ok || h != (Header{Schema: CurrentSchema, WrittenBy: "v0.6.1", Layout: CurrentLayout}) {
		t.Errorf("header = %+v, %v", h, ok)
	}
	// In the layout: devices (must-set) before reputation (optional)
	// before listen and store (defaults), under their banners.
	order := []string{layoutSections[0].Banner, "devices:", layoutSections[1].Banner, "reputation:", layoutSections[2].Banner, "listen:", "store:"}
	last := -1
	for _, s := range order {
		i := strings.Index(out, s)
		if i <= last {
			t.Fatalf("%q is out of layout order:\n%s", s, out)
		}
		last = i
	}

	kinds := map[string][]Change{}
	for _, c := range changes {
		kinds[c.Kind] = append(kinds[c.Kind], c)
	}
	if len(kinds["header"]) != 1 {
		t.Errorf("header changes: %+v", kinds["header"])
	}
	removed := map[string]int{}
	for _, c := range kinds["removed"] {
		removed[c.Key] = c.Line
		if !strings.Contains(c.Note, "removed in v0.2.0") {
			t.Errorf("removal note does not say why: %+v", c)
		}
	}
	if !reflect.DeepEqual(removed, map[string]int{"listen.syslogUdp": 5, "listen.syslogTcp": 6, "store.maxEvents": 12}) {
		t.Errorf("removed = %v, want the three v0.2.0 removals on their original lines", removed)
	}
	if len(kinds["moved"]) == 0 {
		t.Error("no moves reported, yet devices moved above listen")
	}
}

// The example config is the layout: carrying it forward changes
// nothing. This is what holds deploy/config.example.yaml to
// layoutSections' order and banners, and the banners to the example.
func TestExampleConfigIsInTheLayout(t *testing.T) {
	example := readExample(t)
	h, _ := ParseHeader(example)
	out, changes, _ := CarryForward(example, h.WrittenBy)
	if out != example {
		t.Errorf("carrying deploy/config.example.yaml forward changed it -- the example and layoutSections disagree.\nchanges: %+v", changes)
	}
	if len(changes) != 0 {
		t.Errorf("changes on the example: %+v", changes)
	}
}

func TestLayoutCoversEveryTopLevelKey(t *testing.T) {
	inLayout := map[string]bool{}
	for _, sec := range layoutSections {
		for _, k := range sec.Keys {
			if inLayout[k] {
				t.Errorf("%s is in the layout twice", k)
			}
			inLayout[k] = true
		}
	}
	typ := reflect.TypeOf(Config{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0]
		if name != "" && !inLayout[name] {
			t.Errorf("top-level key %s has no place in layoutSections (and so none in deploy/config.example.yaml)", name)
		}
		delete(inLayout, name)
	}
	for k := range inLayout {
		t.Errorf("layoutSections names %s, which is not a top-level Config key", k)
	}
}

func TestCarryForwardIsIdempotent(t *testing.T) {
	once, _, _ := CarryForward(v010Config, "v0.6.1")
	twice, changes, _ := CarryForward(once, "v0.6.1")
	if twice != once || len(changes) != 0 {
		t.Errorf("a second Carry forward changed the file (%+v):\n%s", changes, twice)
	}
}

func TestCarryForwardRenamesInPlace(t *testing.T) {
	removedOrRenamedKeys["listen.webPort"] = removedKey{Version: "v9.9.9", Why: "test rename", RenamedTo: "listen.http"}
	t.Cleanup(func() { delete(removedOrRenamedKeys, "listen.webPort") })

	out, changes, problems := CarryForward("listen:\n  webPort: \":9090\"   # mine\n", "v0.6.1")
	if !strings.Contains(out, `  http: ":9090"   # mine`) {
		t.Errorf("rename not made in place:\n%s", out)
	}
	if hasFatal(problems) {
		t.Errorf("renamed file refused: %+v", problems)
	}
	found := false
	for _, c := range changes {
		if c.Kind == "renamed" && c.Key == "listen.webPort" && c.To == "listen.http" && c.Line == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("no rename change reported: %+v", changes)
	}
}

func TestCarryForwardLeavesSharedLinesAlone(t *testing.T) {
	text := "listen: {syslogUdp: \":1514\", http: \":8080\"}\n"
	out, _, problems := CarryForward(text, "v0.6.1")
	if !strings.Contains(out, `http: ":8080"`) {
		t.Fatalf("a setting sharing a line with a removed one was lost:\n%s", out)
	}
	if p := findProblem(problems, "listen.syslogUdp"); p == nil || p.Line != 1 || !strings.Contains(p.Message, "by hand") {
		t.Errorf("no problem left for the removed key it could not take out: %+v", problems)
	}
}

func TestCarryForwardRefusesUnparseable(t *testing.T) {
	text := "listen:\n  http: [\n"
	out, changes, problems := CarryForward(text, "v0.6.1")
	if out != text || len(changes) != 0 || !hasFatal(problems) {
		t.Errorf("unparseable text: out changed=%v, changes=%v, problems=%v", out != text, changes, problems)
	}
}

func TestStampHeader(t *testing.T) {
	valid := "listen:\n  http: \":8080\"\n"
	if h, _ := ParseHeader(StampHeader(valid, "0.7.0")); h != (Header{Schema: CurrentSchema, WrittenBy: "v0.7.0", Layout: 0}) {
		t.Errorf("valid headerless text stamped %+v", h)
	}
	if h, _ := ParseHeader(StampHeader(v010Config, "0.7.0")); h.Schema != 1 {
		t.Errorf("a text this build refuses was stamped schema %d, want its guess (1)", h.Schema)
	}
	carried, _, _ := CarryForward(v010Config, "v0.6.1")
	if h, _ := ParseHeader(StampHeader(carried, "0.7.0")); h != (Header{Schema: CurrentSchema, WrittenBy: "v0.7.0", Layout: CurrentLayout}) {
		t.Errorf("carried-forward text stamped %+v", h)
	}
}

func TestSetupOnlyConfigSalvagesSections(t *testing.T) {
	raw := []byte(`listen:
  http: ":9443"
auth:
  storePath: /data/accounts.json
oidc:
  issuerUrl: https://idp.example.com
  clientId: mv
  clientSecret: s3cret-value
  publicBaseUrl: https://mv.example.com
  bogusKey: 1
store:
  maxEvents: 5
`)
	got := SetupOnlyConfig(raw, nil)
	if got.Config.Listen.HTTP != ":9443" || got.Config.Auth.StorePath != "/data/accounts.json" {
		t.Errorf("clean sections not used: listen.http=%q auth.storePath=%q", got.Config.Listen.HTTP, got.Config.Auth.StorePath)
	}
	if !got.Broken["oidc"] || !got.Broken["store"] || got.Broken["auth"] {
		t.Errorf("broken = %v, want oidc and store", got.Broken)
	}
	if got.OIDCUsable {
		t.Error("SSO left on although the refusal is inside the oidc block")
	}
	if got.Config.OIDC.IssuerURL != "" {
		t.Error("a broken oidc block leaked part of itself into the salvaged config")
	}

	// A broken auth block means the default accounts store.
	broken := SetupOnlyConfig([]byte("auth:\n  storePath: /x\n  nope: true\n"), nil)
	if broken.Config.Auth.StorePath != defaults().Auth.StorePath {
		t.Errorf("broken auth block gave store path %q, want the default %q", broken.Config.Auth.StorePath, defaults().Auth.StorePath)
	}

	// SSO stays on when the refusal is elsewhere.
	elsewhere := SetupOnlyConfig([]byte(`oidc:
  issuerUrl: https://idp.example.com
  clientId: mv
  clientSecret: s3cret-value
  publicBaseUrl: https://mv.example.com
store:
  maxEvents: 5
`), nil)
	if !elsewhere.OIDCUsable {
		t.Error("SSO turned off although the oidc block is fine")
	}
}
