// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// This file holds the watch window and the seven nights of memory behind
// it (issue #680). Three things live here, and the reasoning for each is
// on its own type: Window (when an entry is expected to see traffic),
// Night (what actually happened on one occurrence of that window), and
// Ring (whether the run of kept nights is currently broken).
//
// The zone on Window is the only local-time concept in this codebase.
// Everything else -- events, matchlog, the API, every timestamp recorded
// here -- stays UTC. "Quiet hours 00:00-06:00" is a claim about the
// operator's night, and in any DST zone a fixed-offset window drifts an
// hour against that for half the year: the watch would start reporting
// empty nights each spring for a reason nobody could see on the screen.
// Storing the IANA name rather than an offset is what makes the window
// mean the same clock time all year. The zone is used solely to turn a
// window into instants; nothing else reads it.

// Clock is a time of day, in minutes since local midnight. 0 is 00:00 and
// 1439 is 23:59; MinutesPerDay itself is not a valid Clock, because a
// window ending "at midnight" ends at 00:00 on the following date, which
// is what End <= Start already expresses (see Window).
type Clock int

// MinutesPerDay bounds Clock.
const MinutesPerDay = 24 * 60

// Valid reports whether c is a time of day this package can turn into an
// instant.
func (c Clock) Valid() bool { return c >= 0 && c < MinutesPerDay }

// String renders c as HH:MM, the form it is stored and displayed in.
func (c Clock) String() string {
	if !c.Valid() {
		return "invalid"
	}
	return fmt.Sprintf("%02d:%02d", int(c)/60, int(c)%60)
}

// ParseClock reads an HH:MM time of day. Deliberately strict -- two
// digits, a colon, two digits -- because this value is authored by an
// operator and then used to decide whether a night was empty, and a
// silently-reinterpreted "6" would move the boundary by six hours.
func ParseClock(s string) (Clock, error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("%w: %q is not HH:MM", ErrInvalidWindow, s)
	}
	for _, i := range [4]int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("%w: %q is not HH:MM", ErrInvalidWindow, s)
		}
	}
	hh := int(s[0]-'0')*10 + int(s[1]-'0')
	mm := int(s[3]-'0')*10 + int(s[4]-'0')
	if hh > 23 || mm > 59 {
		return 0, fmt.Errorf("%w: %q is not a time of day", ErrInvalidWindow, s)
	}
	return Clock(hh*60 + mm), nil
}

// Window is the daily clock range an entry is expected to see traffic in:
// a start and end time of day, the days of the week the window opens on,
// and the IANA zone those clock times are read in.
//
// The interval is half-open, [Start, End), so 00:00-06:00 excludes
// 06:00:00 exactly.
//
// End <= Start means the window runs into the following date, and that is
// the normal case rather than the edge: 22:00-06:00 is eight hours across
// two dates. Start == End means no window at all -- an entry that is
// watched at every hour, which is what a row renders as "always". There is
// no separate "is there a window" flag because there is nothing a
// zero-length window could otherwise mean.
//
// Days empty means every day. Days filters on the date the window
// *opened*, never the date it closed, so a Saturday-night watch running
// 22:00-06:00 is one Saturday night, not a Saturday fragment plus a Sunday
// one.
//
// Zone empty means UTC. See this file's own comment for why an IANA name
// and not an offset.
type Window struct {
	Start Clock          `json:"start,omitzero"`
	End   Clock          `json:"end,omitzero"`
	Days  []time.Weekday `json:"days,omitempty"`
	Zone  string         `json:"zone,omitempty"`
}

// ErrInvalidWindow is returned by Window.Validate (and so by ValidateEntry)
// for a window whose clock times are not times of day, whose days are not
// days of the week, or whose zone this host's tzdata cannot resolve.
var ErrInvalidWindow = errors.New("watchlist: a window needs HH:MM times, real weekdays and a loadable IANA zone")

// Defined reports whether this entry actually has a window. A zero-length
// window is "always" -- see Window.
func (w Window) Defined() bool { return w.Start != w.End }

// Validate rejects a window this package could not turn into instants. A
// zone is checked against the running host's tzdata rather than a name
// pattern: an unloadable zone would otherwise be stored happily and then
// silently fall back to UTC every night after, which is exactly the
// invisible one-hour drift the zone exists to prevent.
func (w Window) Validate() error {
	if !w.Start.Valid() || !w.End.Valid() {
		return fmt.Errorf("%w: start %d and end %d must be minutes within one day", ErrInvalidWindow, w.Start, w.End)
	}
	for _, d := range w.Days {
		if d < time.Sunday || d > time.Saturday {
			return fmt.Errorf("%w: %d is not a day of the week", ErrInvalidWindow, d)
		}
	}
	if _, err := w.Location(); err != nil {
		return fmt.Errorf("%w: zone %q: %v", ErrInvalidWindow, w.Zone, err)
	}
	return nil
}

// Location resolves Zone. An empty zone is UTC, which is also what every
// other timestamp in this codebase is in.
func (w Window) Location() (*time.Location, error) {
	if w.Zone == "" {
		return time.UTC, nil
	}
	return time.LoadLocation(w.Zone)
}

// opensOn reports whether the window opens on a local date, per Days.
func (w Window) opensOn(day time.Time) bool {
	if len(w.Days) == 0 {
		return true
	}
	// Noon, not midnight: there are zones (America/Santiago, for one)
	// where local midnight does not exist on the spring-forward date, and
	// asking a normalized midnight for its weekday can land on the next
	// day. Noon is never in a DST gap.
	wd := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, day.Location()).Weekday()
	for _, d := range w.Days {
		if d == wd {
			return true
		}
	}
	return false
}

// Occurrence is one night: a single opening of the window, as the two UTC
// instants it ran between. Half-open, [Open, Close).
type Occurrence struct {
	Open  time.Time
	Close time.Time
}

// Contains reports whether t falls inside this occurrence.
func (o Occurrence) Contains(t time.Time) bool {
	return !t.Before(o.Open) && t.Before(o.Close)
}

// occurrenceOpeningOn builds the occurrence that opens on the local date
// day, or ok=false if Days excludes that date.
//
// The two instants are built with time.Date in the window's own zone, so a
// DST transition inside the window changes how long the night lasted in
// real time while leaving the clock times it means alone. 00:00-06:00 on
// Europe/London's spring-forward date is five real hours, and 00:00-06:00
// on its autumn date is seven -- both of them still "midnight to six",
// which is the claim the operator made.
func (w Window) occurrenceOpeningOn(day time.Time) (Occurrence, bool) {
	if !w.Defined() || !w.opensOn(day) {
		return Occurrence{}, false
	}
	y, m, d := day.Date()
	loc := day.Location()
	// Minutes rather than hours+minutes: time.Date normalizes an
	// out-of-range field, so passing the whole clock as minutes is the
	// same instant with no arithmetic to get wrong. The closing date
	// overflows by a day when the window crosses midnight, which time.Date
	// normalizes for the same reason.
	closeDay := d
	if w.End <= w.Start {
		closeDay++
	}
	open := time.Date(y, m, d, 0, int(w.Start), 0, 0, loc)
	closes := time.Date(y, m, closeDay, 0, int(w.End), 0, 0, loc)
	return Occurrence{Open: open.UTC(), Close: closes.UTC()}, true
}

// localDate returns t as a date in the window's zone, at noon -- see
// opensOn for why noon.
func localDate(t time.Time, loc *time.Location, addDays int) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day()+addDays, 12, 0, 0, 0, loc)
}

// maxLookbackDays bounds the date walk in ClosedSince and Occurrence
// lookups. A window that opens on one weekday only needs seven dates per
// night, so MaxNights weeks plus a fortnight of slack covers every shape
// this package can express, and the bound is what keeps a lazy fill after
// a long outage O(constant) rather than O(time the app was down).
const maxLookbackDays = MaxNights*7 + 14

// OccurrenceAt returns the occurrence of w that contains t, if any. A t
// outside every occurrence is ok=false: it says nothing about any night.
func (w Window) OccurrenceAt(t time.Time) (Occurrence, bool) {
	if !w.Defined() {
		return Occurrence{}, false
	}
	loc, err := w.Location()
	if err != nil {
		return Occurrence{}, false
	}
	// At most two dates can produce an occurrence containing t: today's,
	// and -- when the window crosses midnight -- yesterday's.
	for back := 0; back <= 1; back++ {
		o, ok := w.occurrenceOpeningOn(localDate(t, loc, -back))
		if ok && o.Contains(t) {
			return o, true
		}
	}
	return Occurrence{}, false
}

// ClosedSince returns the occurrences of w that have closed at or before
// now and opened strictly after after, oldest first, at most limit of them
// (the most recent, when there are more).
//
// after is the last night already recorded, which is what makes the fill
// idempotent: an occurrence is returned once, and never again once it has
// been written down.
func (w Window) ClosedSince(after, now time.Time, limit int) []Occurrence {
	if !w.Defined() || limit <= 0 {
		return nil
	}
	loc, err := w.Location()
	if err != nil {
		return nil
	}
	var found []Occurrence
	for back := 0; back < maxLookbackDays && len(found) < limit; back++ {
		o, ok := w.occurrenceOpeningOn(localDate(now, loc, -back))
		if !ok {
			continue
		}
		if !o.Open.After(after) {
			// Dates only get older from here, so no earlier occurrence
			// can be newer than what is already recorded.
			break
		}
		if o.Close.After(now) {
			continue // still open, or not yet closed
		}
		found = append(found, o)
	}
	// Collected newest-first; nights are stored oldest-first.
	sort.Slice(found, func(i, j int) bool { return found[i].Open.Before(found[j].Open) })
	return found
}

// MarshalJSON renders a Clock as "HH:MM" rather than a minute count, in
// the stored definition and over the API alike. The number is what this
// package computes with; the clock time is what an operator wrote and
// what the UI shows, and a persisted blob a human reads back should say
// "22:00" rather than 1320.
func (c Clock) MarshalJSON() ([]byte, error) {
	if !c.Valid() {
		return nil, fmt.Errorf("%w: %d is not a time of day", ErrInvalidWindow, int(c))
	}
	return []byte(`"` + c.String() + `"`), nil
}

// UnmarshalJSON reads the "HH:MM" form MarshalJSON writes.
func (c *Clock) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		return nil
	}
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return fmt.Errorf("%w: %s is not a quoted HH:MM time", ErrInvalidWindow, s)
	}
	parsed, err := ParseClock(s[1 : len(s)-1])
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}
