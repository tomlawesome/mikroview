// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

// SetupOnly is what setup-only mode (#1347) can still use from a config
// start-up refused: enough to put the listener where the operator
// expects it, open the right accounts store, and decide whether SSO can
// stay on.
type SetupOnly struct {
	// Config is the defaults, overlaid with every top-level section of
	// the refused file that decoded cleanly on its own, then the
	// environment, flags and app folder as load applies them. A broken
	// section keeps its defaults -- so a broken auth block means the
	// default accounts store path, as the owner ruled.
	Config Config
	// Broken lists the top-level sections that did not decode.
	Broken map[string]bool
	// OIDCUsable is true when the oidc block decoded and config.Validate
	// found nothing wrong inside it (owner, #1347 answer 7a: SSO stays on
	// unless the refusal is inside the oidc block).
	OIDCUsable bool
}

// SetupOnlyConfig salvages a refused config, section by section. raw is
// the file as read (nil or unparseable leaves every section at its
// default); args are the process's flags.
func SetupOnlyConfig(raw []byte, args []string) SetupOnly {
	out := SetupOnly{Broken: map[string]bool{}}
	cfg := defaults()

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		out.Broken["(whole file)"] = true
	} else if root := documentRoot(&doc); root != nil && root.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(root.Content); i += 2 {
			key := root.Content[i].Value
			if decodeSection(&cfg, root.Content[i], root.Content[i+1]) != nil {
				out.Broken[key] = true
			}
		}
	} else if root != nil {
		out.Broken["(whole file)"] = true
	}

	applyEnv(&cfg)
	_ = applyFlags(&cfg, args)
	_, _ = applyAppFolder(&cfg)
	cfg.normaliseDevices()
	result := cfg.Validate()

	out.OIDCUsable = !out.Broken["oidc"] && !out.Broken["(whole file)"]
	for _, p := range append(result.Fatal, result.Warnings...) {
		if p.Key == "oidc" || strings.HasPrefix(p.Key, "oidc.") {
			out.OIDCUsable = false
		}
	}
	out.Config = cfg
	return out
}

// decodeSection decodes one top-level key and its value, strictly
// (unknown keys refuse), into a fresh defaults() and copies just that
// section across -- so a section that fails half way leaves nothing of
// itself behind in cfg, not even in a map the two would otherwise share.
func decodeSection(cfg *Config, key, value *yaml.Node) error {
	one := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{key, value}}
	b, err := yaml.Marshal(one)
	if err != nil {
		return err
	}
	trial := defaults()
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&trial); err != nil {
		return err
	}
	t := reflect.TypeOf(trial)
	for i := 0; i < t.NumField(); i++ {
		if strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0] == key.Value {
			reflect.ValueOf(cfg).Elem().Field(i).Set(reflect.ValueOf(trial).Field(i))
		}
	}
	return nil
}
