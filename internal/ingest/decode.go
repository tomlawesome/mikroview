// SPDX-License-Identifier: AGPL-3.0-only

package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"
)

// maxFieldLen bounds every string field a record may carry. Mirrors
// internal/routeros.maxFieldLen's value and reasoning: this input is
// authenticated by an ingest token, but the token only proves which
// device sent it, not that the bytes are well-formed -- a compromised or
// simply buggy router-side script controls this content entirely, the
// same footing a syslog line is on. Kept at the same value so a rule or
// host label behaves identically wherever it ends up, whether it arrived
// via a log line or via this path.
const maxFieldLen = 256

// maxRecordsPerPage bounds how many records one page may carry, on top
// of the ~64KiB whole-body limit the endpoint enforces (the number
// RouterOS's own /tool fetch imposes -- see
// docs/decisions/routeros-ingest-spike.md). 500 rules measured at ~132
// bytes each is already close to what the byte cap alone limits this to;
// 1000 is headroom against a page that is small in bytes but pathological
// in record count (many records with near-empty fields), not a number
// meant to be reached in practice.
const maxRecordsPerPage = 1000

// maxListItems bounds how many entries one RouterOSList field may carry
// -- a WireGuard peer's allowed addresses, a rule's connection-state
// set. The whole-body limit already bounds this loosely, but a cap on
// the field itself is what this package does everywhere else, and the
// legitimate numbers are tiny: RouterOS has five connection states, and
// a peer routing 64 separate subnets is already far past any real
// deployment. Refused whole rather than truncated, like every other cap
// here.
const maxListItems = 64

// maxPages bounds Payload.Pages so a malformed or malicious value (a
// page claiming to be one of a billion) can't make a downstream
// page-tracking or staleness scheme allocate or iterate proportionally
// to an attacker-chosen number.
const maxPages = 1000

var (
	// ErrUnknownKind is returned by DecodePayload for a Kind this build
	// does not recognise -- refused rather than accepted-and-ignored, so
	// a typo or a future kind pushed at an old build fails loudly instead
	// of silently applying nothing.
	ErrUnknownKind = errors.New("ingest: unrecognised kind")
	// ErrBadPage is returned for a page/pages combination outside
	// [1, pages] with pages capped at maxPages.
	ErrBadPage = errors.New("ingest: page/pages out of range")
	// errTrailingData is returned when the body contains more than one
	// JSON value -- e.g. a second object concatenated after the first --
	// which a plain single Decode call would silently ignore.
	errTrailingData = errors.New("ingest: trailing data after the JSON payload")
)

// wireFormat is the envelope every payload arrives as. Records is
// deliberately raw: which concrete type it decodes into depends on Kind,
// decided after the envelope itself has been validated.
type wireFormat struct {
	Kind    Kind            `json:"kind"`
	Page    int             `json:"page"`
	Pages   int             `json:"pages"`
	Records json.RawMessage `json:"records"`
}

// Payload is one fully decoded and validated page of RouterOS state.
// Exactly one of the record slices below is populated, matching Kind --
// a caller switches on Kind and reads the matching field. This shape
// (rather than a caller-invoked accessor per Kind) means a Payload
// returned by DecodePayload has already had its records strictly decoded
// and validated; there is no second step a caller could forget.
type Payload struct {
	Kind  Kind
	Page  int
	Pages int

	AddressList         []AddressListEntry
	FilterRules         []FilterRule
	NATRules            []NATRule
	DNSStatic           []DNSStaticEntry
	DHCPLeases          []DHCPLease
	ARP                 []ARPEntry
	WireguardInterfaces []WireguardInterface
	WireguardPeers      []WireguardPeer
}

// RecordCount returns how many records are in whichever slice matches
// Kind -- a small convenience for a caller that wants to log or report
// "how much arrived" (e.g. an audit entry) without a type switch over
// every kind of its own.
func (p Payload) RecordCount() int {
	switch p.Kind {
	case KindAddressList:
		return len(p.AddressList)
	case KindFilterRule:
		return len(p.FilterRules)
	case KindNATRule:
		return len(p.NATRules)
	case KindDNSStatic:
		return len(p.DNSStatic)
	case KindDHCPLease:
		return len(p.DHCPLeases)
	case KindARP:
		return len(p.ARP)
	case KindWireguardInterface:
		return len(p.WireguardInterfaces)
	case KindWireguardPeer:
		return len(p.WireguardPeers)
	default:
		return 0
	}
}

// DecodePayload strictly decodes and validates a RouterOS ingest payload
// from r. Strict in the sense AGENTS.md asks of anything parsing
// attacker-shaped input: unknown fields are refused rather than silently
// ignored, every bound in this file is enforced before a Payload is ever
// returned, and a payload that fails any check is refused whole rather
// than partially applied -- this package has no concept of "apply what
// parsed and skip the rest."
func DecodePayload(r io.Reader) (Payload, error) {
	var wire wireFormat
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&wire); err != nil {
		return Payload{}, fmt.Errorf("ingest: decoding payload: %w", err)
	}
	// A second Decode call succeeding (rather than returning io.EOF)
	// means the body held more than one JSON value -- e.g. an object
	// concatenated after the real one. A plain single Decode call would
	// silently accept the first and ignore the rest, which is not
	// "strict" by this package's own definition.
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return Payload{}, errTrailingData
	}

	if wire.Pages < 1 || wire.Pages > maxPages || wire.Page < 1 || wire.Page > wire.Pages {
		return Payload{}, ErrBadPage
	}

	out := Payload{Kind: wire.Kind, Page: wire.Page, Pages: wire.Pages}

	var err error
	switch wire.Kind {
	case KindAddressList:
		out.AddressList, err = decodeRecords[AddressListEntry](wire.Records)
	case KindFilterRule:
		out.FilterRules, err = decodeRecords[FilterRule](wire.Records)
	case KindNATRule:
		out.NATRules, err = decodeRecords[NATRule](wire.Records)
	case KindDNSStatic:
		out.DNSStatic, err = decodeRecords[DNSStaticEntry](wire.Records)
	case KindDHCPLease:
		out.DHCPLeases, err = decodeRecords[DHCPLease](wire.Records)
	case KindARP:
		out.ARP, err = decodeRecords[ARPEntry](wire.Records)
	case KindWireguardInterface:
		out.WireguardInterfaces, err = decodeRecords[WireguardInterface](wire.Records)
	case KindWireguardPeer:
		out.WireguardPeers, err = decodeRecords[WireguardPeer](wire.Records)
	default:
		return Payload{}, ErrUnknownKind
	}
	if err != nil {
		return Payload{}, err
	}
	return out, nil
}

// recordValidator is implemented by every record type -- see each type's
// validate method below, all of which just bound their own string
// fields via validateFieldText. RouterOSInt fields validate themselves
// during UnmarshalJSON, so there is nothing left for validate to check
// on those.
type recordValidator interface {
	validate() error
}

// decodeRecords strictly decodes raw into a []T (T a record type) and
// validates every element, refusing the whole page on the first failure
// -- see DecodePayload's doc comment on why partial application isn't a
// path this package offers.
func decodeRecords[T recordValidator](raw json.RawMessage) ([]T, error) {
	if len(raw) == 0 {
		return nil, errors.New("ingest: records is required")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var records []T
	if err := dec.Decode(&records); err != nil {
		return nil, fmt.Errorf("ingest: decoding records: %w", err)
	}
	if len(records) > maxRecordsPerPage {
		return nil, fmt.Errorf("ingest: %d records exceeds the %d-per-page limit", len(records), maxRecordsPerPage)
	}
	for i, rec := range records {
		if err := rec.validate(); err != nil {
			return nil, fmt.Errorf("ingest: record %d: %w", i, err)
		}
	}
	return records, nil
}

// validateFieldText applies the same three checks
// internal/entities.validateEntityText applies to admin-typed text, to
// text a router pushed instead: valid UTF-8, no control or Unicode
// format characters (the bidi-override class used in real-world spoofing
// attacks -- see internal/auth.ValidateUsername for the CVE references
// entities.validateEntityText carries), and a bounded length. This data
// ends up in the same places admin-typed labels do -- the UI, the audit
// trail, exported CSVs -- so it is held to the same rule.
func validateFieldText(field, s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("ingest: %s is not valid UTF-8", field)
	}
	if utf8.RuneCountInString(s) > maxFieldLen {
		return fmt.Errorf("ingest: %s exceeds %d characters", field, maxFieldLen)
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return fmt.Errorf("ingest: %s contains a control or formatting character", field)
		}
	}
	return nil
}

// validateFieldList applies validateFieldText to every element of a
// list-shaped field, and bounds how many there may be -- a field that
// holds a set is still router-controlled text, one bound short of the
// scalar case.
func validateFieldList(field string, l RouterOSList) error {
	if len(l) > maxListItems {
		return fmt.Errorf("ingest: %s carries %d entries, over the %d limit", field, len(l), maxListItems)
	}
	for _, v := range l {
		if err := validateFieldText(field, v); err != nil {
			return err
		}
	}
	return nil
}

func (e AddressListEntry) validate() error {
	if err := validateFieldText("list", e.List); err != nil {
		return err
	}
	if err := validateFieldText("address", e.Address); err != nil {
		return err
	}
	return validateFieldText("comment", e.Comment)
}

func (r FilterRule) validate() error {
	if err := validateFieldText("comment", r.Comment); err != nil {
		return err
	}
	if err := validateFieldText("chain", r.Chain); err != nil {
		return err
	}
	if err := validateFieldText("action", r.Action); err != nil {
		return err
	}
	if err := validateFieldText("srcAddressList", r.SrcAddressList); err != nil {
		return err
	}
	if err := validateFieldText("logPrefix", r.LogPrefix); err != nil {
		return err
	}
	if err := validateFieldText("dstPort", string(r.DstPort)); err != nil {
		return err
	}
	if err := validateFieldText("protocol", r.Protocol); err != nil {
		return err
	}
	// dstAddress/srcAddress arrived with #274 without a line here, so
	// they were the two record fields in this package reaching the UI,
	// the exports and the audit trail unbounded and unscreened for the
	// control and bidi-override characters every other field is screened
	// for. Noticed while adding the #408 fields beside them.
	if err := validateFieldText("dstAddress", r.DstAddress); err != nil {
		return err
	}
	if err := validateFieldText("srcAddress", r.SrcAddress); err != nil {
		return err
	}
	if err := validateFieldList("connectionState", r.ConnectionState); err != nil {
		return err
	}
	if err := validateFieldText("inInterface", r.InInterface); err != nil {
		return err
	}
	return validateFieldText("outInterface", r.OutInterface)
}

func (r NATRule) validate() error {
	if err := validateFieldText("comment", r.Comment); err != nil {
		return err
	}
	if err := validateFieldText("chain", r.Chain); err != nil {
		return err
	}
	if err := validateFieldText("action", r.Action); err != nil {
		return err
	}
	if err := validateFieldText("toAddresses", r.ToAddresses); err != nil {
		return err
	}
	if err := validateFieldText("toPorts", string(r.ToPorts)); err != nil {
		return err
	}
	if err := validateFieldText("dstPort", string(r.DstPort)); err != nil {
		return err
	}
	if err := validateFieldText("protocol", r.Protocol); err != nil {
		return err
	}
	if err := validateFieldText("inInterface", r.InInterface); err != nil {
		return err
	}
	if err := validateFieldText("outInterface", r.OutInterface); err != nil {
		return err
	}
	if err := validateFieldText("srcAddress", r.SrcAddress); err != nil {
		return err
	}
	return validateFieldText("dstAddress", r.DstAddress)
}

func (e DNSStaticEntry) validate() error {
	if err := validateFieldText("name", e.Name); err != nil {
		return err
	}
	return validateFieldText("address", e.Address)
}

func (l DHCPLease) validate() error {
	if err := validateFieldText("hostname", l.Hostname); err != nil {
		return err
	}
	if err := validateFieldText("mac", l.MAC); err != nil {
		return err
	}
	return validateFieldText("address", l.Address)
}

func (a ARPEntry) validate() error {
	if err := validateFieldText("address", a.Address); err != nil {
		return err
	}
	return validateFieldText("mac", a.MAC)
}

func (w WireguardInterface) validate() error {
	if err := validateFieldText("name", w.Name); err != nil {
		return err
	}
	if err := validateFieldText("comment", w.Comment); err != nil {
		return err
	}
	return validateFieldText("publicKey", w.PublicKey)
}

func (p WireguardPeer) validate() error {
	if err := validateFieldText("publicKey", p.PublicKey); err != nil {
		return err
	}
	if err := validateFieldList("allowedAddress", p.AllowedAddress); err != nil {
		return err
	}
	if err := validateFieldText("endpointAddress", p.EndpointAddress); err != nil {
		return err
	}
	return validateFieldText("comment", p.Comment)
}
