// SPDX-License-Identifier: AGPL-3.0-only

// Command routerosfreshness answers the one question
// scripts/routeros-freshness.sh needs from Go: is a candidate RouterOS
// version newer than the release mikroview's command knowledge has been
// reviewed against?
//
// A tiny program rather than the shell script parsing versions itself,
// so there is one comparison in this repository and the scheduled check
// cannot disagree with what the product believes (#436).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tomlawesome/mikroview/internal/routeros"
)

func main() {
	printReviewed := flag.Bool("print-reviewed", false, "print the reviewed RouterOS version and exit")
	candidate := flag.String("candidate", "", "a RouterOS version to compare against the reviewed one")
	flag.Parse()

	if *printReviewed {
		fmt.Println(routeros.ReviewedVersion)
		return
	}
	if *candidate == "" {
		fmt.Fprintln(os.Stderr, "routerosfreshness: -candidate or -print-reviewed is required")
		os.Exit(2)
	}

	newer, err := routeros.CompareToReviewed(*candidate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "routerosfreshness: %v\n", err)
		os.Exit(2)
	}
	// Exit status is the answer: 0 when the candidate is not newer than
	// what has been reviewed (nothing to do), 1 when it is.
	if newer {
		os.Exit(1)
	}
}
