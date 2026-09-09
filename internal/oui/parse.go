// SPDX-License-Identifier: AGPL-3.0-only

package oui

import (
	"encoding/csv"
	"errors"
	"io"
	"strings"
)

// maxEntries bounds the parsed table the same way every other buffer in
// this codebase has an explicit ceiling. The MA-L registry held ~40,000
// rows when this was written and grows by tens per week, so this is a
// safety net against a corrupted or substituted body, not a limit
// normal use approaches.
var maxEntries = 250_000

// parseCSV reads the IEEE MA-L public listing.
//
// The file's shape, verbatim from the first two lines of the real
// download:
//
//	Registry,Assignment,Organization Name,Organization Address
//	MA-L,286FB9,"Nokia Shanghai Bell Co., Ltd.","No.388 Ning Qiao Road,..."
//
// Names and addresses are quoted when they contain commas, which many
// do, so this uses encoding/csv rather than splitting on commas -- a
// hand-rolled split silently truncates "Cisco Systems, Inc" to "Cisco
// Systems" and there is no way to notice from the result.
//
// Rows are kept only when the registry column says MA-L and the
// assignment is exactly six hex digits: this feed fetches only the MA-L
// file, and a row of any other shape is not something to guess about.
// A malformed row is skipped rather than failing the whole parse, since
// one bad line should not cost an operator the other forty thousand;
// an unreadable *file* still fails, so a truncated download cannot
// quietly replace a good table with a short one.
func parseCSV(body []byte) (map[string]string, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(body), "\ufeff")))
	// The address column contains stray quotes in a handful of rows;
	// LazyQuotes keeps those rows readable instead of aborting the file
	// at whichever row IEEE last fat-fingered.
	r.LazyQuotes = true
	// Rows vary in field count (a private listing has an empty address,
	// some have a trailing field), so the count is not a contract.
	r.FieldsPerRecord = -1

	entries := make(map[string]string, 48_000)
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				continue // one unreadable row, not an unreadable file
			}
			return nil, err
		}
		if len(rec) < 3 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(rec[0]), "MA-L") {
			continue // the header row and anything else land here
		}
		assignment := strings.ToUpper(strings.TrimSpace(rec[1]))
		if !isHex6(assignment) {
			continue
		}
		name := strings.TrimSpace(rec[2])
		if name == "" {
			continue
		}
		if _, seen := entries[assignment]; !seen && len(entries) >= maxEntries {
			break
		}
		entries[assignment] = name
	}
	if len(entries) == 0 {
		return nil, errors.New("no MA-L assignments found")
	}
	return entries, nil
}

// isHex6 reports whether s is exactly six uppercase hex digits.
func isHex6(s string) bool {
	if len(s) != 6 {
		return false
	}
	for i := 0; i < 6; i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
