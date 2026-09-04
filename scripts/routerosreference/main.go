// SPDX-License-Identifier: AGPL-3.0-only
//
// Checks internal/routeros/reference/menus.json against MikroTik's own
// published CLI Reference: every console path mikroview drives must
// still be a path MikroTik documents.
//
// It deliberately stores nothing of theirs. The index is fetched, the
// handful of paths mikroview needs are looked up in it, and the index is
// thrown away -- we keep only the list of commands we use, which is a
// fact about mikroview rather than a copy of their reference.
//
// The index comes from the documentation site's sitemap: one request,
// rather than crawling a thousand pages.
//
// What this cannot tell you: whether a command parses. #924 was a
// console grammar error -- [find !dynamic ...] instead of
// [find where !dynamic ...] -- on a menu that exists and is spelled
// correctly, and no path check would have caught it. That is what the
// CHR exercise is for. This catches the other failure: a menu that
// moved or was renamed under us.
//
// Usage: go run ./scripts/routerosreference
// Exit 0: every path we use is documented.
// Exit 1: at least one is not -- it names which.
// Exit 2: could not check (network, or the sitemap changed shape).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	sitemapURL    = "https://manual.mikrotik.com/sitemap.xml"
	docPrefix     = "https://manual.mikrotik.com/docs/cli-reference/"
	referencePath = "internal/routeros/reference/menus.json"
)

// Menu is one console menu mikroview drives.
type Menu struct {
	Path       string   `json:"path"`
	Console    string   `json:"console"`
	UsedFor    string   `json:"usedFor"`
	Properties []string `json:"properties"`
	Verified   string   `json:"verified"`
}

// Reference is the on-disk shape of menus.json.
type Reference struct {
	Description    string `json:"description"`
	CheckedAgainst string `json:"checkedAgainst"`
	CheckedOn      string `json:"checkedOn"`
	Menus          []Menu `json:"menus"`
}

var locRE = regexp.MustCompile(`<loc>(.*?)</loc>`)

// documentedPaths returns the console paths MikroTik's CLI Reference
// publishes, as a set.
func documentedPaths() (map[string]bool, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(sitemapURL)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", sitemapURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: unexpected status %d", sitemapURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", sitemapURL, err)
	}

	paths := map[string]bool{}
	for _, m := range locRE.FindAllStringSubmatch(string(body), -1) {
		loc := m[1]
		if !strings.HasPrefix(loc, docPrefix) {
			continue
		}
		if p := strings.Trim(strings.TrimPrefix(loc, docPrefix), "/"); p != "" {
			paths[p] = true
		}
	}
	if len(paths) == 0 {
		// An empty answer must fail rather than pass everything: a
		// sitemap that changed shape would otherwise report every path
		// as fine by finding nothing to contradict them.
		return nil, fmt.Errorf("no cli-reference paths found in the sitemap -- has its URL layout changed?")
	}
	return paths, nil
}

func main() {
	blob, err := os.ReadFile(referencePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "routerosreference: reading %s: %v\n", referencePath, err)
		os.Exit(2)
	}
	var ref Reference
	if err := json.Unmarshal(blob, &ref); err != nil {
		fmt.Fprintf(os.Stderr, "routerosreference: parsing %s: %v\n", referencePath, err)
		os.Exit(2)
	}

	documented, err := documentedPaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "routerosreference: %v\n", err)
		os.Exit(2)
	}

	var missing []string
	for _, m := range ref.Menus {
		if !documented[m.Path] {
			missing = append(missing, fmt.Sprintf("%s (%s)", m.Path, m.Console))
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "routerosreference: %d of %d paths are no longer documented by MikroTik:\n", len(missing), len(ref.Menus))
		for _, m := range missing {
			fmt.Fprintf(os.Stderr, "  %s\n", m)
		}
		fmt.Fprintf(os.Stderr, "A renamed or removed menu means mikroview is telling operators to run something that no longer exists.\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "routerosreference: all %d paths mikroview uses are documented (checked %d published paths)\n", len(ref.Menus), len(documented))
}
