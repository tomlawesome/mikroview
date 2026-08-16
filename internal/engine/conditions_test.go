// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// fakeMembers is a trivial AddressListMembership for tests -- a set of
// (device, list, ip) tuples that are "in" the list, everything else is
// not.
type fakeMembers map[[3]string]bool

func (m fakeMembers) InAddressList(device, list, ip string) bool {
	return m[[3]string{device, list, ip}]
}

func mustCompile(t *testing.T, c Condition) conditionMatcher {
	t.Helper()
	fn, err := compileCondition(c)
	if err != nil {
		t.Fatalf("compileCondition(%+v): %v", c, err)
	}
	return fn
}

// --- closed sets ---

func TestValidateConditionRejectsUnknownField(t *testing.T) {
	err := ValidateCondition(Condition{Field: "nonsense", Operator: OpEquals, Values: []string{"x"}})
	if err == nil {
		t.Fatal("ValidateCondition succeeded on an unknown field, want a hard failure")
	}
}

func TestValidateConditionRejectsUnknownOperator(t *testing.T) {
	err := ValidateCondition(Condition{Field: FieldChain, Operator: "sideways", Values: []string{"x"}})
	if err == nil {
		t.Fatal("ValidateCondition succeeded on an unknown operator, want a hard failure")
	}
}

// TestFieldOperatorCompatibilityIsClosed pins which operators each field
// accepts -- the "enumerated in one place" contract #402 asks for, made
// concrete and regression-tested rather than only prose in a doc
// comment.
func TestFieldOperatorCompatibilityIsClosed(t *testing.T) {
	cases := []struct {
		field   Field
		allowed []Operator
	}{
		{FieldSourceAddress, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet, OpInCIDR, OpInRange, OpMatchesClassification}},
		{FieldDestinationAddress, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet, OpInCIDR, OpInRange, OpMatchesClassification}},
		{FieldSourcePort, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet, OpInRange}},
		{FieldDestinationPort, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet, OpInRange}},
		{FieldProtocol, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet}},
		{FieldAction, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet}},
		{FieldChain, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet}},
		{FieldRuleLabel, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet}},
		{FieldConnectionState, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet}},
		{FieldInInterface, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet}},
		{FieldOutInterface, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet}},
		{FieldDayOfWeek, []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet}},
		{FieldAddressListMembership, []Operator{OpEquals, OpNotEquals}},
		{FieldTimeOfDay, []Operator{OpInRange}},
	}
	allOps := []Operator{OpEquals, OpNotEquals, OpInSet, OpNotInSet, OpInCIDR, OpInRange, OpMatchesClassification}

	for _, tc := range cases {
		allowedSet := make(map[Operator]bool, len(tc.allowed))
		for _, op := range tc.allowed {
			allowedSet[op] = true
		}
		for _, op := range allOps {
			want := allowedSet[op]
			got := fieldOperators[tc.field][op]
			if got != want {
				t.Errorf("field %q operator %q: allowed=%v, want %v", tc.field, op, got, want)
			}
		}
	}
}

// --- addresses ---

func TestConditionAddressEquals(t *testing.T) {
	fn := mustCompile(t, Condition{Field: FieldSourceAddress, Operator: OpEquals, Values: []string{"198.51.100.1"}})
	if !fn(store.Event{SrcIP: "198.51.100.1"}, nil) {
		t.Error("want match on equal source address")
	}
	if fn(store.Event{SrcIP: "198.51.100.2"}, nil) {
		t.Error("want no match on different source address")
	}
}

func TestConditionAddressInCIDROrsAcrossValues(t *testing.T) {
	fn := mustCompile(t, Condition{
		Field: FieldDestinationAddress, Operator: OpInCIDR,
		Values: []string{"10.0.0.0/8", "203.0.113.0/24"},
	})
	if !fn(store.Event{DstIP: "10.1.2.3"}, nil) {
		t.Error("want match in first CIDR")
	}
	if !fn(store.Event{DstIP: "203.0.113.9"}, nil) {
		t.Error("want match in second CIDR (OR across Values)")
	}
	if fn(store.Event{DstIP: "8.8.8.8"}, nil) {
		t.Error("want no match outside every CIDR")
	}
}

func TestConditionAddressInRange(t *testing.T) {
	fn := mustCompile(t, Condition{
		Field: FieldSourceAddress, Operator: OpInRange,
		Values: []string{"198.51.100.10", "198.51.100.20"},
	})
	if !fn(store.Event{SrcIP: "198.51.100.15"}, nil) {
		t.Error("want match inside range")
	}
	if fn(store.Event{SrcIP: "198.51.100.25"}, nil) {
		t.Error("want no match outside range")
	}
}

func TestConditionAddressMatchesClassification(t *testing.T) {
	fn := mustCompile(t, Condition{Field: FieldSourceAddress, Operator: OpMatchesClassification, Values: []string{"internal"}})
	if !fn(store.Event{SrcIP: "192.168.1.5"}, nil) {
		t.Error("want match: private address is internal")
	}
	if fn(store.Event{SrcIP: "8.8.8.8"}, nil) {
		t.Error("want no match: public address is not internal")
	}

	fnExt := mustCompile(t, Condition{Field: FieldSourceAddress, Operator: OpMatchesClassification, Values: []string{"external"}})
	if !fnExt(store.Event{SrcIP: "8.8.8.8"}, nil) {
		t.Error("want match: public address is external")
	}
}

func TestConditionAddressRejectsInvalidIP(t *testing.T) {
	if err := ValidateCondition(Condition{Field: FieldSourceAddress, Operator: OpEquals, Values: []string{"not-an-ip"}}); err == nil {
		t.Fatal("ValidateCondition succeeded with an invalid IP, want a hard failure")
	}
}

// --- ports ---

func TestConditionPortInSet(t *testing.T) {
	fn := mustCompile(t, Condition{Field: FieldDestinationPort, Operator: OpInSet, Values: []string{"22", "3389"}})
	if !fn(store.Event{DstPort: 22}, nil) || !fn(store.Event{DstPort: 3389}, nil) {
		t.Error("want match on any listed port")
	}
	if fn(store.Event{DstPort: 80}, nil) {
		t.Error("want no match on an unlisted port")
	}
}

func TestConditionPortNotInSet(t *testing.T) {
	fn := mustCompile(t, Condition{Field: FieldDestinationPort, Operator: OpNotInSet, Values: []string{"80", "443"}})
	if fn(store.Event{DstPort: 80}, nil) {
		t.Error("want no match on a listed port under notInSet")
	}
	if !fn(store.Event{DstPort: 22}, nil) {
		t.Error("want match on an unlisted port under notInSet")
	}
}

func TestConditionPortInRange(t *testing.T) {
	fn := mustCompile(t, Condition{Field: FieldSourcePort, Operator: OpInRange, Values: []string{"1024", "65535"}})
	if fn(store.Event{SrcPort: 80}, nil) {
		t.Error("want no match below range")
	}
	if !fn(store.Event{SrcPort: 50000}, nil) {
		t.Error("want match inside range")
	}
}

func TestConditionPortRejectsOutOfRangeValue(t *testing.T) {
	if err := ValidateCondition(Condition{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"70000"}}); err == nil {
		t.Fatal("ValidateCondition succeeded with an out-of-range port, want a hard failure")
	}
}

// --- plain string fields ---

func TestConditionActionEqualsNegation(t *testing.T) {
	fn := mustCompile(t, Condition{Field: FieldAction, Operator: OpNotEquals, Values: []string{string(store.ActionAccept)}})
	if fn(store.Event{Action: store.ActionAccept}, nil) {
		t.Error("want no match: notEquals on an equal value")
	}
	if !fn(store.Event{Action: store.ActionDrop}, nil) {
		t.Error("want match: notEquals on a different value")
	}
}

func TestConditionChainInSet(t *testing.T) {
	fn := mustCompile(t, Condition{Field: FieldChain, Operator: OpInSet, Values: []string{"forward", "input"}})
	if !fn(store.Event{Chain: "input"}, nil) {
		t.Error("want match")
	}
	if fn(store.Event{Chain: "output"}, nil) {
		t.Error("want no match")
	}
}

// --- address list membership ---

func TestConditionAddressListMembershipEquals(t *testing.T) {
	members := fakeMembers{{"router1", "blocked", "198.51.100.9"}: true}
	fn := mustCompile(t, Condition{Field: FieldAddressListMembership, Operator: OpEquals, Values: []string{"router1", "blocked"}})

	if !fn(store.Event{SrcIP: "198.51.100.9"}, members) {
		t.Error("want match: address is a member")
	}
	if fn(store.Event{SrcIP: "198.51.100.10"}, members) {
		t.Error("want no match: address is not a member")
	}
}

func TestConditionAddressListMembershipNotEquals(t *testing.T) {
	members := fakeMembers{{"router1", "blocked", "198.51.100.9"}: true}
	fn := mustCompile(t, Condition{Field: FieldAddressListMembership, Operator: OpNotEquals, Values: []string{"router1", "blocked"}})

	if fn(store.Event{SrcIP: "198.51.100.9"}, members) {
		t.Error("want no match: address IS a member, notEquals should fail")
	}
	if !fn(store.Event{SrcIP: "198.51.100.10"}, members) {
		t.Error("want match: address is not a member")
	}
}

// TestConditionAddressListMembershipNilMembersIsSafeDirection pins the
// same "cannot verify -> do not claim a match" rule
// watchlist.MatchWithLists uses when members is nil: neither equals nor
// notEquals may fire on a membership question the definition has no way
// to answer.
func TestConditionAddressListMembershipNilMembersIsSafeDirection(t *testing.T) {
	fnEq := mustCompile(t, Condition{Field: FieldAddressListMembership, Operator: OpEquals, Values: []string{"router1", "blocked"}})
	fnNotEq := mustCompile(t, Condition{Field: FieldAddressListMembership, Operator: OpNotEquals, Values: []string{"router1", "blocked"}})

	e := store.Event{SrcIP: "198.51.100.9"}
	if fnEq(e, nil) {
		t.Error("equals must not match with nil members")
	}
	if fnNotEq(e, nil) {
		t.Error("notEquals must not match with nil members either -- unresolved membership is not a match in either direction")
	}
}

func TestConditionAddressListMembershipRequiresDeviceAndList(t *testing.T) {
	if err := ValidateCondition(Condition{Field: FieldAddressListMembership, Operator: OpEquals, Values: []string{"only-one"}}); err == nil {
		t.Fatal("ValidateCondition succeeded with one value, want exactly [device, list]")
	}
}

// --- time of day / day of week ---

func TestConditionTimeOfDayWithinWindow(t *testing.T) {
	fn := mustCompile(t, Condition{Field: FieldTimeOfDay, Operator: OpInRange, Values: []string{"09:00", "17:00"}})

	inWindow := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	outWindow := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	if !fn(store.Event{ReceivedAt: inWindow}, nil) {
		t.Error("want match inside window")
	}
	if fn(store.Event{ReceivedAt: outWindow}, nil) {
		t.Error("want no match outside window")
	}
}

// TestConditionTimeOfDayWrapsMidnight pins the off-hours shape
// (23:00-06:00) where start > end.
func TestConditionTimeOfDayWrapsMidnight(t *testing.T) {
	fn := mustCompile(t, Condition{Field: FieldTimeOfDay, Operator: OpInRange, Values: []string{"23:00", "06:00"}})

	late := time.Date(2026, 8, 16, 23, 30, 0, 0, time.UTC)
	early := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	midday := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	if !fn(store.Event{ReceivedAt: late}, nil) {
		t.Error("want match late in the evening")
	}
	if !fn(store.Event{ReceivedAt: early}, nil) {
		t.Error("want match early in the morning")
	}
	if fn(store.Event{ReceivedAt: midday}, nil) {
		t.Error("want no match at midday")
	}
}

func TestConditionTimeOfDayOnlyAcceptsInRange(t *testing.T) {
	err := ValidateCondition(Condition{Field: FieldTimeOfDay, Operator: OpEquals, Values: []string{"09:00"}})
	if err == nil {
		t.Fatal("ValidateCondition succeeded with equals on timeOfDay, want a hard failure -- only inRange is meaningful")
	}
}

func TestConditionDayOfWeekInSetCaseInsensitive(t *testing.T) {
	fn := mustCompile(t, Condition{Field: FieldDayOfWeek, Operator: OpInSet, Values: []string{"Saturday", "sunday"}})

	sat := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) // a Saturday
	mon := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC) // a Monday
	if !fn(store.Event{ReceivedAt: sat}, nil) {
		t.Error("want match on Saturday")
	}
	if fn(store.Event{ReceivedAt: mon}, nil) {
		t.Error("want no match on Monday")
	}
}

func TestConditionDayOfWeekRejectsUnknownName(t *testing.T) {
	if err := ValidateCondition(Condition{Field: FieldDayOfWeek, Operator: OpEquals, Values: []string{"Funday"}}); err == nil {
		t.Fatal("ValidateCondition succeeded with an invalid day name, want a hard failure")
	}
}

// --- compileConditions: AND across fields, duplicate-field rejection ---

func TestCompileConditionsANDsAcrossFields(t *testing.T) {
	cs, err := compileConditions([]Condition{
		{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}},
		{Field: FieldAction, Operator: OpEquals, Values: []string{string(store.ActionDrop)}},
	})
	if err != nil {
		t.Fatalf("compileConditions: %v", err)
	}
	if !cs.match(store.Event{DstPort: 22, Action: store.ActionDrop}, nil) {
		t.Error("want match: both conditions satisfied")
	}
	if cs.match(store.Event{DstPort: 22, Action: store.ActionAccept}, nil) {
		t.Error("want no match: only one of two AND'd conditions satisfied")
	}
}

func TestCompileConditionsRejectsEmptyList(t *testing.T) {
	if _, err := compileConditions(nil); err == nil {
		t.Fatal("compileConditions succeeded on an empty condition list, want a hard failure")
	}
}

func TestCompileConditionsRejectsDuplicateField(t *testing.T) {
	_, err := compileConditions([]Condition{
		{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}},
		{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"23"}},
	})
	if err == nil {
		t.Fatal("compileConditions succeeded with two conditions on the same field, want a hard failure")
	}
}
