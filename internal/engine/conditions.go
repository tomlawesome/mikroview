// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
)

// AddressListMembership answers whether an address is in a router's
// address list right now -- the same shape watchlist.AddressListMembership
// declares (internal/watchlist/watchlist.go). A local interface rather
// than an import of internal/watchlist, the same dependency-direction
// reasoning that package's own doc comment gives for not importing
// internal/routerstate: internal/watchlist is expected to collapse onto
// this chassis (docs/decisions/evaluation-engine.md, #405), so this
// package importing it now would risk exactly the cycle that move is
// meant to resolve, not create. Any concrete implementation of
// watchlist.AddressListMembership already satisfies this interface
// without adornment, since Go interfaces are structural.
type AddressListMembership interface {
	InAddressList(device, list, ip string) bool
}

// Field is the closed set of event attributes a declarative Condition
// may match against (issue #402, docs/decisions/evaluation-engine.md
// section 2). This is the entire vocabulary a condition can name --
// adding a new Field is a code change to this file, never a value an
// operator or a future API boundary can introduce on its own. See this
// package's own doc comment (engine.go) for the ADR sentence this
// closedness exists to make good on.
type Field string

const (
	FieldSourceAddress         Field = "sourceAddress"
	FieldDestinationAddress    Field = "destinationAddress"
	FieldSourcePort            Field = "sourcePort"
	FieldDestinationPort       Field = "destinationPort"
	FieldProtocol              Field = "protocol"
	FieldAction                Field = "action"
	FieldChain                 Field = "chain"
	FieldRuleLabel             Field = "ruleLabel"
	FieldAddressListMembership Field = "addressListMembership"
	// FieldSourceIdentity matches an event's *device* identity rather
	// than one of its address fields: MAC-preferred, IP as fallback,
	// matchlog.Identity's rule (#243 section 1), which is not the same
	// question FieldSourceAddress asks.
	//
	// The difference is load-bearing, not stylistic. An expectation
	// scoped to a MAC must keep matching when its device's IP changes
	// under DHCP -- that is the entire reason a MAC-bound identity
	// exists. And, symmetrically, an expectation scoped to an IP must
	// NOT match an event that carries a source MAC, even when that
	// event's IP is the right one: matchlog collapses on the
	// MAC-preferred key, so treating those as the same device would let
	// one device's match history split or merge depending on which chain
	// a particular log line arrived on. FieldSourceAddress cannot express
	// either half. See Condition's doc comment for the Values shape.
	FieldSourceIdentity  Field = "sourceIdentity"
	FieldConnectionState Field = "connectionState"
	FieldInInterface     Field = "inInterface"
	FieldOutInterface    Field = "outInterface"
	FieldTimeOfDay       Field = "timeOfDay"
	FieldDayOfWeek       Field = "dayOfWeek"
)

// Operator is the closed set of comparisons a Condition may apply to its
// Field -- see Condition's own doc comment for which operators each
// Field accepts and what Values means under each.
type Operator string

const (
	OpEquals                Operator = "equals"
	OpNotEquals             Operator = "notEquals"
	OpInSet                 Operator = "inSet"
	OpNotInSet              Operator = "notInSet"
	OpInCIDR                Operator = "inCIDR"
	OpInRange               Operator = "inRange"
	OpMatchesClassification Operator = "matchesClassification"
)

// Condition is one (field, operator, value) match test -- the entire
// match language a declarative definition may express. There is no
// expression syntax, no user-supplied regex, no arithmetic: see this
// package's doc comment (engine.go) for the ADR's own words on why.
//
// Values holds the operand(s); its meaning depends on Operator:
//
//   - OpEquals / OpNotEquals: exactly one entry.
//   - OpInSet / OpNotInSet: one or more entries, matching if the event's
//     field value equals ANY of them -- this is where within-a-field OR
//     lives. A Condition list is AND'd together (see compileConditions);
//     each Condition's own Values is OR'd internally, the same rule
//     issue #44 established for Scope's own axes
//     (docs/decisions/evaluation-engine.md).
//   - OpInCIDR: one or more CIDR strings (address fields only), matching
//     if the address falls in ANY of them.
//   - OpInRange: exactly two entries, [low, high], inclusive. Decimal
//     for ports, dotted/colon IP text compared by byte order for
//     addresses, "HH:MM" for time of day (wraparound handled when
//     low > high -- see inClockRange).
//   - OpMatchesClassification: exactly one entry, one of "internal",
//     "external" or "any" -- store.Scope's own vocabulary
//     (internal/store/query.go), address fields only.
//
// FieldAddressListMembership is special: Values is exactly
// [device, list], naming the router and address list to check
// (watchlist.AddressListMembership.InAddressList) against the event's
// source address -- the only field this package resolves membership
// for, since nothing in the closed field set is "arbitrary address,
// caller's choice." Only OpEquals ("is a member") and OpNotEquals ("is
// not a member") apply to it.
//
// FieldSourceIdentity is special in the same way: Values is exactly
// [mac, ip], the stored identity to compare the event's own resolved
// identity against, and at least one of the two must be non-empty (an
// identity with neither names no device at all -- matchlog refuses to
// store or query one, see matchlog.ErrEmptyIdentity). Only OpEquals
// ("is this device") and OpNotEquals ("is not this device") apply.
//
// One Condition per Field is enforced by compileConditions -- a
// duplicate field is a construction-time error, not a second AND'd test
// (use InSet/InCIDR's own OR for multiple values on the same field).
type Condition struct {
	Field    Field    `json:"field"`
	Operator Operator `json:"operator"`
	Values   []string `json:"values"`
}

// ValidateCondition checks c's structural validity -- field known,
// operator accepted by that field, Values well-formed and parseable --
// without constructing anything that evaluates it. Mirrors
// ValidateScope/ValidateParams's standalone-validation shape
// (definition.go, params.go) for a future API boundary that wants to
// validate a condition before it is ever attached to a definition.
func ValidateCondition(c Condition) error {
	_, err := compileCondition(c)
	return err
}

// conditionMatcher is a compiled Condition, ready to test one event
// without re-parsing Values -- built once at DeclarativeDefinition
// construction (see compileConditions), not on the per-event hot path,
// since the dispatch benchmark gate (#402) depends on conditions costing
// no more than a closure call and a map/slice lookup.
type conditionMatcher func(e store.Event, members AddressListMembership) bool

// compiledConditionSet is every Condition a DeclarativeDefinition
// carries, already compiled -- AND'd together in match (within-field OR
// already lives inside each conditionMatcher, see Condition's doc
// comment).
type compiledConditionSet []conditionMatcher

func (cs compiledConditionSet) match(e store.Event, members AddressListMembership) bool {
	for _, fn := range cs {
		if !fn(e, members) {
			return false
		}
	}
	return true
}

// compileConditions validates and compiles conds, rejecting an empty
// list (a declarative definition with no conditions matches nothing
// meaningfully, and is almost certainly a mistake, not a valid "match
// everything" definition) and a duplicate field (see Condition's doc
// comment on why that isn't the AND/OR model this package implements).
func compileConditions(conds []Condition) (compiledConditionSet, error) {
	if len(conds) == 0 {
		return nil, fmt.Errorf("engine: a declarative definition requires at least one condition")
	}
	seen := make(map[Field]struct{}, len(conds))
	out := make(compiledConditionSet, 0, len(conds))
	for _, c := range conds {
		if _, dup := seen[c.Field]; dup {
			return nil, fmt.Errorf("engine: duplicate condition on field %q -- one condition per field, use inSet/inCIDR for multiple values", c.Field)
		}
		seen[c.Field] = struct{}{}
		fn, err := compileCondition(c)
		if err != nil {
			return nil, err
		}
		out = append(out, fn)
	}
	return out, nil
}

// fieldOperators is the closed field x operator compatibility table --
// the one place that decides which operators mean anything for a given
// field. Consulted by compileCondition before doing any per-operator
// work, so an unsupported combination fails with one clear error rather
// than a field-specific compiler quietly ignoring the operator.
var fieldOperators = map[Field]map[Operator]bool{
	FieldSourceAddress:         addressOperators,
	FieldDestinationAddress:    addressOperators,
	FieldSourcePort:            portOperators,
	FieldDestinationPort:       portOperators,
	FieldProtocol:              setOperators,
	FieldAction:                setOperators,
	FieldChain:                 setOperators,
	FieldRuleLabel:             setOperators,
	FieldConnectionState:       setOperators,
	FieldInInterface:           setOperators,
	FieldOutInterface:          setOperators,
	FieldDayOfWeek:             setOperators,
	FieldAddressListMembership: membershipOperators,
	FieldSourceIdentity:        membershipOperators,
	FieldTimeOfDay:             timeOfDayOperators,
}

var (
	addressOperators = map[Operator]bool{
		OpEquals: true, OpNotEquals: true, OpInSet: true, OpNotInSet: true,
		OpInCIDR: true, OpInRange: true, OpMatchesClassification: true,
	}
	portOperators = map[Operator]bool{
		OpEquals: true, OpNotEquals: true, OpInSet: true, OpNotInSet: true, OpInRange: true,
	}
	setOperators = map[Operator]bool{
		OpEquals: true, OpNotEquals: true, OpInSet: true, OpNotInSet: true,
	}
	membershipOperators = map[Operator]bool{
		OpEquals: true, OpNotEquals: true,
	}
	timeOfDayOperators = map[Operator]bool{
		OpInRange: true,
	}
)

func compileCondition(c Condition) (conditionMatcher, error) {
	allowed, ok := fieldOperators[c.Field]
	if !ok {
		return nil, fmt.Errorf("engine: unknown condition field %q", c.Field)
	}
	if !allowed[c.Operator] {
		return nil, fmt.Errorf("engine: field %q does not accept operator %q", c.Field, c.Operator)
	}

	switch c.Field {
	case FieldSourceAddress, FieldDestinationAddress:
		return compileAddressCondition(c)
	case FieldSourcePort, FieldDestinationPort:
		return compilePortCondition(c)
	case FieldAddressListMembership:
		return compileMembershipCondition(c)
	case FieldSourceIdentity:
		return compileSourceIdentityCondition(c)
	case FieldTimeOfDay:
		return compileTimeOfDayCondition(c)
	case FieldDayOfWeek:
		return compileDayOfWeekCondition(c)
	default:
		return compileSetCondition(c)
	}
}

// ---- ports ----

func portFieldValue(f Field, e store.Event) int {
	if f == FieldSourcePort {
		return e.SrcPort
	}
	return e.DstPort
}

func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("engine: invalid port value %q: %w", s, err)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("engine: port %d out of range [1,65535]", n)
	}
	return n, nil
}

func compilePortCondition(c Condition) (conditionMatcher, error) {
	field := c.Field
	switch c.Operator {
	case OpEquals, OpNotEquals:
		if len(c.Values) != 1 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants exactly one value, got %d", c.Operator, field, len(c.Values))
		}
		p, err := parsePort(c.Values[0])
		if err != nil {
			return nil, err
		}
		negate := c.Operator == OpNotEquals
		return func(e store.Event, _ AddressListMembership) bool {
			return (portFieldValue(field, e) == p) != negate
		}, nil
	case OpInSet, OpNotInSet:
		if len(c.Values) == 0 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants at least one value", c.Operator, field)
		}
		set := make(map[int]struct{}, len(c.Values))
		for _, v := range c.Values {
			p, err := parsePort(v)
			if err != nil {
				return nil, err
			}
			set[p] = struct{}{}
		}
		negate := c.Operator == OpNotInSet
		return func(e store.Event, _ AddressListMembership) bool {
			_, hit := set[portFieldValue(field, e)]
			return hit != negate
		}, nil
	case OpInRange:
		if len(c.Values) != 2 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants exactly two values [low, high], got %d", c.Operator, field, len(c.Values))
		}
		lo, err := parsePort(c.Values[0])
		if err != nil {
			return nil, err
		}
		hi, err := parsePort(c.Values[1])
		if err != nil {
			return nil, err
		}
		if lo > hi {
			return nil, fmt.Errorf("engine: field %q inRange low %d exceeds high %d", field, lo, hi)
		}
		return func(e store.Event, _ AddressListMembership) bool {
			v := portFieldValue(field, e)
			return v >= lo && v <= hi
		}, nil
	default:
		return nil, fmt.Errorf("engine: field %q does not accept operator %q", field, c.Operator)
	}
}

// ---- plain string fields (protocol, action, chain, ruleLabel,
// connectionState, in/out interface, dayOfWeek's set operators share
// the same shape but not the same extractor, see compileDayOfWeekCondition) ----

func stringExtractor(f Field) (func(store.Event) string, error) {
	switch f {
	case FieldProtocol:
		return func(e store.Event) string { return e.Protocol }, nil
	case FieldAction:
		return func(e store.Event) string { return string(e.Action) }, nil
	case FieldChain:
		return func(e store.Event) string { return e.Chain }, nil
	case FieldRuleLabel:
		return func(e store.Event) string { return e.RuleLabel }, nil
	case FieldConnectionState:
		return func(e store.Event) string { return e.ConnState }, nil
	case FieldInInterface:
		return func(e store.Event) string { return e.InInterface }, nil
	case FieldOutInterface:
		return func(e store.Event) string { return e.OutInterface }, nil
	default:
		return nil, fmt.Errorf("engine: field %q is not a plain string field", f)
	}
}

func compileSetCondition(c Condition) (conditionMatcher, error) {
	extract, err := stringExtractor(c.Field)
	if err != nil {
		return nil, err
	}
	switch c.Operator {
	case OpEquals, OpNotEquals:
		if len(c.Values) != 1 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants exactly one value, got %d", c.Operator, c.Field, len(c.Values))
		}
		val := c.Values[0]
		negate := c.Operator == OpNotEquals
		return func(e store.Event, _ AddressListMembership) bool {
			return (extract(e) == val) != negate
		}, nil
	case OpInSet, OpNotInSet:
		if len(c.Values) == 0 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants at least one value", c.Operator, c.Field)
		}
		set := make(map[string]struct{}, len(c.Values))
		for _, v := range c.Values {
			set[v] = struct{}{}
		}
		negate := c.Operator == OpNotInSet
		return func(e store.Event, _ AddressListMembership) bool {
			_, hit := set[extract(e)]
			return hit != negate
		}, nil
	default:
		return nil, fmt.Errorf("engine: field %q does not accept operator %q", c.Field, c.Operator)
	}
}

// ---- addresses ----

func addressFieldValue(f Field, e store.Event) string {
	if f == FieldSourceAddress {
		return e.SrcIP
	}
	return e.DstIP
}

// isPublicIP mirrors internal/store's own scopeMatches helper (unexported
// there) -- this package keeps its own small copy rather than exporting
// one across a package boundary solely for this, the same "each package
// keeps its own small private copy" precedent internal/detect and
// internal/watchlist already set (see internal/store/query.go's own
// comment on isPublicIP).
func isPublicIP(ip net.IP) bool {
	return !ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast()
}

func classificationMatches(class, addr string) bool {
	if class == "any" {
		return true
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	public := isPublicIP(ip)
	if class == "internal" {
		return !public
	}
	return public // "external"
}

func compileAddressCondition(c Condition) (conditionMatcher, error) {
	field := c.Field
	switch c.Operator {
	case OpEquals, OpNotEquals:
		if len(c.Values) != 1 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants exactly one value, got %d", c.Operator, field, len(c.Values))
		}
		val := c.Values[0]
		if net.ParseIP(val) == nil {
			return nil, fmt.Errorf("engine: %q is not a valid IP address", val)
		}
		negate := c.Operator == OpNotEquals
		return func(e store.Event, _ AddressListMembership) bool {
			return (addressFieldValue(field, e) == val) != negate
		}, nil
	case OpInSet, OpNotInSet:
		if len(c.Values) == 0 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants at least one value", c.Operator, field)
		}
		set := make(map[string]struct{}, len(c.Values))
		for _, v := range c.Values {
			if net.ParseIP(v) == nil {
				return nil, fmt.Errorf("engine: %q is not a valid IP address", v)
			}
			set[v] = struct{}{}
		}
		negate := c.Operator == OpNotInSet
		return func(e store.Event, _ AddressListMembership) bool {
			_, hit := set[addressFieldValue(field, e)]
			return hit != negate
		}, nil
	case OpInCIDR:
		if len(c.Values) == 0 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants at least one value", c.Operator, field)
		}
		nets := make([]*net.IPNet, 0, len(c.Values))
		for _, v := range c.Values {
			_, ipNet, err := net.ParseCIDR(v)
			if err != nil {
				return nil, fmt.Errorf("engine: %q is not a valid CIDR: %w", v, err)
			}
			nets = append(nets, ipNet)
		}
		return func(e store.Event, _ AddressListMembership) bool {
			ip := net.ParseIP(addressFieldValue(field, e))
			if ip == nil {
				return false
			}
			for _, n := range nets {
				if n.Contains(ip) {
					return true
				}
			}
			return false
		}, nil
	case OpInRange:
		if len(c.Values) != 2 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants exactly two values [low, high], got %d", c.Operator, field, len(c.Values))
		}
		lo := net.ParseIP(c.Values[0])
		hi := net.ParseIP(c.Values[1])
		if lo == nil || hi == nil {
			return nil, fmt.Errorf("engine: inRange values must be valid IP addresses, got %v", c.Values)
		}
		lo16, hi16 := lo.To16(), hi.To16()
		if bytes.Compare(lo16, hi16) > 0 {
			return nil, fmt.Errorf("engine: field %q inRange low %s exceeds high %s", field, c.Values[0], c.Values[1])
		}
		return func(e store.Event, _ AddressListMembership) bool {
			ip := net.ParseIP(addressFieldValue(field, e))
			if ip == nil {
				return false
			}
			b := ip.To16()
			return bytes.Compare(b, lo16) >= 0 && bytes.Compare(b, hi16) <= 0
		}, nil
	case OpMatchesClassification:
		if len(c.Values) != 1 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants exactly one value, got %d", c.Operator, field, len(c.Values))
		}
		class := c.Values[0]
		switch class {
		case "internal", "external", "any":
		default:
			return nil, fmt.Errorf("engine: %q is not a valid classification (want internal, external or any)", class)
		}
		return func(e store.Event, _ AddressListMembership) bool {
			return classificationMatches(class, addressFieldValue(field, e))
		}, nil
	default:
		return nil, fmt.Errorf("engine: field %q does not accept operator %q", field, c.Operator)
	}
}

// ---- address-list membership ----

func compileMembershipCondition(c Condition) (conditionMatcher, error) {
	if c.Operator != OpEquals && c.Operator != OpNotEquals {
		return nil, fmt.Errorf("engine: field %q does not accept operator %q", c.Field, c.Operator)
	}
	if len(c.Values) != 2 {
		return nil, fmt.Errorf("engine: field %q wants exactly two values [device, list], got %d", c.Field, len(c.Values))
	}
	device, list := c.Values[0], c.Values[1]
	if device == "" || list == "" {
		return nil, fmt.Errorf("engine: field %q requires non-empty device and list", c.Field)
	}
	negate := c.Operator == OpNotEquals
	return func(e store.Event, members AddressListMembership) bool {
		// members == nil (or no source address to check) is the safe
		// direction, same as watchlist.MatchWithLists: without a way to
		// answer "is this address in that list", the honest answer is
		// not to claim a match either way.
		if members == nil || e.SrcIP == "" {
			return false
		}
		member := members.InAddressList(device, list, e.SrcIP)
		return member != negate
	}, nil
}

// ---- source identity ----

// compileSourceIdentityCondition compiles a FieldSourceIdentity test --
// see that field's own doc comment. The comparison itself is
// matchlog.Identity.MatchesSource, called rather than reimplemented: its
// own doc comment records that Append and Query each implemented the
// MAC-preferred rule separately once and drifted, which is why the
// preference (and the MAC lowercasing a real RouterOS makes necessary)
// lives in exactly one place.
func compileSourceIdentityCondition(c Condition) (conditionMatcher, error) {
	if c.Operator != OpEquals && c.Operator != OpNotEquals {
		return nil, fmt.Errorf("engine: field %q does not accept operator %q", c.Field, c.Operator)
	}
	if len(c.Values) != 2 {
		return nil, fmt.Errorf("engine: field %q wants exactly two values [mac, ip], got %d", c.Field, len(c.Values))
	}
	id := matchlog.Identity{MAC: c.Values[0], IP: c.Values[1]}
	if id.Empty() {
		return nil, fmt.Errorf("engine: field %q requires a mac or an ip -- an identity with neither names no device", c.Field)
	}
	negate := c.Operator == OpNotEquals
	return func(e store.Event, _ AddressListMembership) bool {
		candidate := eventIdentity(e)
		if candidate.Empty() {
			// Nothing to attribute this event to at all -- neither a
			// match nor a non-match, and the safe direction is the same
			// one compileMembershipCondition takes: claim nothing.
			return false
		}
		return id.MatchesSource(candidate) != negate
	}, nil
}

// ---- time of day / day of week ----

func parseClock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("engine: invalid time-of-day value %q, want \"HH:MM\": %w", s, err)
	}
	return t.Hour()*60 + t.Minute(), nil
}

func clockMinutes(t time.Time) int { return t.Hour()*60 + t.Minute() }

// inClockRange reports whether cur (minutes since midnight) falls in
// [start, end] inclusive -- wrapping across midnight when start > end
// (e.g. 23:00-06:00), the same shape internal/detect.Config's own
// OffHoursStartHour/EndHour window uses.
func inClockRange(start, end, cur int) bool {
	if start <= end {
		return cur >= start && cur <= end
	}
	return cur >= start || cur <= end
}

func compileTimeOfDayCondition(c Condition) (conditionMatcher, error) {
	if c.Operator != OpInRange {
		return nil, fmt.Errorf("engine: field %q only accepts operator %q", c.Field, OpInRange)
	}
	if len(c.Values) != 2 {
		return nil, fmt.Errorf("engine: field %q wants exactly two values [start, end] as \"HH:MM\", got %d", c.Field, len(c.Values))
	}
	start, err := parseClock(c.Values[0])
	if err != nil {
		return nil, err
	}
	end, err := parseClock(c.Values[1])
	if err != nil {
		return nil, err
	}
	return func(e store.Event, _ AddressListMembership) bool {
		return inClockRange(start, end, clockMinutes(e.ReceivedAt))
	}, nil
}

var weekdayNames = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
	"wednesday": time.Wednesday, "thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
}

func parseWeekday(s string) (time.Weekday, error) {
	wd, ok := weekdayNames[strings.ToLower(s)]
	if !ok {
		return 0, fmt.Errorf("engine: %q is not a valid day of week", s)
	}
	return wd, nil
}

func compileDayOfWeekCondition(c Condition) (conditionMatcher, error) {
	switch c.Operator {
	case OpEquals, OpNotEquals:
		if len(c.Values) != 1 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants exactly one value, got %d", c.Operator, c.Field, len(c.Values))
		}
		wd, err := parseWeekday(c.Values[0])
		if err != nil {
			return nil, err
		}
		negate := c.Operator == OpNotEquals
		return func(e store.Event, _ AddressListMembership) bool {
			return (e.ReceivedAt.Weekday() == wd) != negate
		}, nil
	case OpInSet, OpNotInSet:
		if len(c.Values) == 0 {
			return nil, fmt.Errorf("engine: operator %q on field %q wants at least one value", c.Operator, c.Field)
		}
		set := make(map[time.Weekday]struct{}, len(c.Values))
		for _, v := range c.Values {
			wd, err := parseWeekday(v)
			if err != nil {
				return nil, err
			}
			set[wd] = struct{}{}
		}
		negate := c.Operator == OpNotInSet
		return func(e store.Event, _ AddressListMembership) bool {
			_, hit := set[e.ReceivedAt.Weekday()]
			return hit != negate
		}, nil
	default:
		return nil, fmt.Errorf("engine: field %q does not accept operator %q", c.Field, c.Operator)
	}
}

// distinctFieldValue extracts f's value from e as a comparable string,
// for CountingDistinct's DistinctRing -- a deliberately narrower set of
// fields than the full condition vocabulary (see
// distinctCountableFields): timeOfDay and addressListMembership answer a
// yes/no question about an event, not a value with enough distinct
// members to count the breadth of, so they are not meaningful distinct-
// counting keys and are rejected at DeclarativeDefinition construction
// time rather than silently never firing here.
func distinctFieldValue(f Field, e store.Event) (string, bool) {
	switch f {
	case FieldSourceAddress:
		return e.SrcIP, e.SrcIP != ""
	case FieldDestinationAddress:
		return e.DstIP, e.DstIP != ""
	case FieldSourcePort:
		if e.SrcPort == 0 {
			return "", false
		}
		return strconv.Itoa(e.SrcPort), true
	case FieldDestinationPort:
		if e.DstPort == 0 {
			return "", false
		}
		return strconv.Itoa(e.DstPort), true
	case FieldProtocol:
		return e.Protocol, e.Protocol != ""
	case FieldAction:
		return string(e.Action), e.Action != ""
	case FieldChain:
		return e.Chain, e.Chain != ""
	case FieldRuleLabel:
		return e.RuleLabel, e.RuleLabel != ""
	case FieldConnectionState:
		return e.ConnState, true // "" is itself a meaningful, distinct connection state here
	case FieldInInterface:
		return e.InInterface, e.InInterface != ""
	case FieldOutInterface:
		return e.OutInterface, e.OutInterface != ""
	case FieldDayOfWeek:
		return e.ReceivedAt.Weekday().String(), true
	default:
		return "", false
	}
}

var distinctCountableFields = map[Field]bool{
	FieldSourceAddress: true, FieldDestinationAddress: true,
	FieldSourcePort: true, FieldDestinationPort: true,
	FieldProtocol: true, FieldAction: true, FieldChain: true,
	FieldRuleLabel: true, FieldConnectionState: true,
	FieldInInterface: true, FieldOutInterface: true, FieldDayOfWeek: true,
}
