// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// mirrorSelfTriggering lists the only workflows under .github/workflows
// that may run on their own (push, pull_request, schedule, release, ...)
// on the GitHub mirror. This is "The mirror rule" in
// ~/claude/skills/delivery/references/gitlab-first.md (owner decision
// 2026-11-01, following orbit ai/orbit#948 and ai/orbit#1037): the mirror
// never builds or tests what GitLab already did, because a second build
// of the same commit on other runners can disagree with the tested one.
// secret-scan and routeros-freshness are check-only; they build and
// publish nothing.
var mirrorSelfTriggering = map[string]bool{
	"publish-from-gitlab.yml": true,
	"release-on-tag.yml":      true,
	"codeql.yml":              true,
	"secret-scan.yml":         true,
	"routeros-freshness.yml":  true,
}

// TestMirrorWorkflowsOnlyDispatch (#1464) holds every other workflow --
// anything that builds, publishes or deploys -- to a bare
// workflow_dispatch trigger, so a push to the mirror never rebuilds or
// republishes what GitLab already tested.
func TestMirrorWorkflowsOnlyDispatch(t *testing.T) {
	for _, path := range workflowFiles(t) {
		name := filepath.Base(path)
		if mirrorSelfTriggering[name] {
			continue
		}
		var doc struct {
			On yaml.Node `yaml:"on"`
		}
		if err := yaml.Unmarshal(readFile(t, path), &doc); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got := triggerNames(&doc.On)
		if len(got) != 1 || got[0] != "workflow_dispatch" {
			t.Errorf("%s triggers on %v; a workflow outside the mirror rule's allowed list may only have workflow_dispatch", name, got)
		}
	}
}

// TestMirrorWorkflowsMapJobsToGitLab (#1464) requires each dispatch-only
// workflow's header comment to say which GitLab job replaces each of its
// jobs, one line per job id such as "publish -> gate:image", so nobody
// has to guess whether a dispatch-only workflow is dead code.
func TestMirrorWorkflowsMapJobsToGitLab(t *testing.T) {
	for _, path := range workflowFiles(t) {
		name := filepath.Base(path)
		if mirrorSelfTriggering[name] {
			continue
		}
		data := readFile(t, path)
		var doc struct {
			Jobs yaml.Node `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		header := leadingComment(data)
		var ids []string
		for i := 0; i+1 < len(doc.Jobs.Content); i += 2 {
			ids = append(ids, doc.Jobs.Content[i].Value)
		}
		for _, id := range ids {
			re := regexp.MustCompile(`(?m)\b` + regexp.QuoteMeta(id) + `\s*->\s*[A-Za-z0-9_][A-Za-z0-9_:./-]*`)
			if !re.MatchString(header) {
				t.Errorf("%s: header comment does not map job %q to a GitLab job (want a line like %q)", name, id, id+" -> gate:image")
			}
		}
	}
}

// TestMirrorWorkflowSet (#1464) pins the replacement of docker.yml by
// publish-from-gitlab.yml (copies the GitLab-tested digest, never
// rebuilds) and release-on-tag.yml, as orbit does.
func TestMirrorWorkflowSet(t *testing.T) {
	if _, err := os.Stat(".github/workflows/docker.yml"); err == nil {
		t.Error(".github/workflows/docker.yml still exists; the mirror rule replaces it with publish-from-gitlab.yml and release-on-tag.yml")
	}
	for _, name := range []string{"publish-from-gitlab.yml", "release-on-tag.yml"} {
		if _, err := os.Stat(filepath.Join(".github/workflows", name)); err != nil {
			t.Errorf(".github/workflows/%s is missing: %v", name, err)
		}
	}
}

func workflowFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no workflows found under .github/workflows: %v", err)
	}
	sort.Strings(paths)
	return paths
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// triggerNames returns the event names under a workflow's on: key,
// whether written as a scalar, a list or a mapping.
func triggerNames(n *yaml.Node) []string {
	var out []string
	switch n.Kind {
	case yaml.ScalarNode:
		out = append(out, n.Value)
	case yaml.SequenceNode:
		for _, c := range n.Content {
			out = append(out, c.Value)
		}
	case yaml.MappingNode:
		for i := 0; i < len(n.Content); i += 2 {
			out = append(out, n.Content[i].Value)
		}
	}
	sort.Strings(out)
	return out
}

// leadingComment returns the comment lines before the first key that
// is not a "name:" line (the header block of a workflow file).
func leadingComment(data []byte) string {
	var b strings.Builder
	s := bufio.NewScanner(bytes.NewReader(data))
	for s.Scan() {
		line := s.Text()
		switch {
		case strings.HasPrefix(line, "#"):
			b.WriteString(strings.TrimPrefix(line, "#") + "\n")
		case strings.TrimSpace(line) == "" || strings.HasPrefix(line, "name:"):
		default:
			return b.String()
		}
	}
	return b.String()
}
