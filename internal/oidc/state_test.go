package oidc

import (
	"testing"
	"time"
)

func TestNewFlowStateProducesDistinctValues(t *testing.T) {
	now := time.Now()
	a, err := NewFlowState(now)
	if err != nil {
		t.Fatalf("NewFlowState: %v", err)
	}
	b, err := NewFlowState(now)
	if err != nil {
		t.Fatalf("NewFlowState: %v", err)
	}
	if a.State == b.State {
		t.Error("two FlowStates got the same State token")
	}
	if a.Nonce == b.Nonce {
		t.Error("two FlowStates got the same Nonce")
	}
	if a.CodeVerifier == b.CodeVerifier {
		t.Error("two FlowStates got the same PKCE CodeVerifier")
	}
}

func TestFlowStateRoundTrip(t *testing.T) {
	codec, err := NewStateCodec()
	if err != nil {
		t.Fatalf("NewStateCodec: %v", err)
	}
	now := time.Now()
	fs, err := NewFlowState(now)
	if err != nil {
		t.Fatalf("NewFlowState: %v", err)
	}

	encoded, err := codec.Encode(fs)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := codec.Decode(encoded, 10*time.Minute, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded.State != fs.State || decoded.Nonce != fs.Nonce || decoded.CodeVerifier != fs.CodeVerifier {
		t.Errorf("Decode round-trip mismatch: got %+v, want %+v", decoded, fs)
	}
}

func TestFlowStateDecodeRejectsTamperedCiphertext(t *testing.T) {
	codec, err := NewStateCodec()
	if err != nil {
		t.Fatalf("NewStateCodec: %v", err)
	}
	fs, err := NewFlowState(time.Now())
	if err != nil {
		t.Fatalf("NewFlowState: %v", err)
	}
	encoded, err := codec.Encode(fs)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Flip the last character -- corrupts the base64 tail / GCM auth
	// tag without necessarily breaking base64 decodability outright,
	// exercising the AEAD-open failure path specifically rather than
	// just the base64-parse failure path below.
	tampered := []byte(encoded)
	tampered[len(tampered)-1] ^= 1
	if _, err := codec.Decode(string(tampered), 10*time.Minute, time.Now()); err != ErrFlowStateInvalid {
		t.Errorf("Decode(tampered) = %v, want ErrFlowStateInvalid", err)
	}
}

func TestFlowStateDecodeRejectsGarbage(t *testing.T) {
	codec, err := NewStateCodec()
	if err != nil {
		t.Fatalf("NewStateCodec: %v", err)
	}
	if _, err := codec.Decode("not-valid-base64!!!", 10*time.Minute, time.Now()); err != ErrFlowStateInvalid {
		t.Errorf("Decode(garbage) = %v, want ErrFlowStateInvalid", err)
	}
}

func TestFlowStateDecodeRejectsExpired(t *testing.T) {
	codec, err := NewStateCodec()
	if err != nil {
		t.Fatalf("NewStateCodec: %v", err)
	}
	issuedAt := time.Now()
	fs, err := NewFlowState(issuedAt)
	if err != nil {
		t.Fatalf("NewFlowState: %v", err)
	}
	encoded, err := codec.Encode(fs)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	tooLate := issuedAt.Add(11 * time.Minute)
	if _, err := codec.Decode(encoded, 10*time.Minute, tooLate); err != ErrFlowStateInvalid {
		t.Errorf("Decode(expired) = %v, want ErrFlowStateInvalid", err)
	}
}

func TestFlowStateDecodeRejectsWrongCodecKey(t *testing.T) {
	codecA, err := NewStateCodec()
	if err != nil {
		t.Fatalf("NewStateCodec: %v", err)
	}
	codecB, err := NewStateCodec()
	if err != nil {
		t.Fatalf("NewStateCodec: %v", err)
	}
	fs, err := NewFlowState(time.Now())
	if err != nil {
		t.Fatalf("NewFlowState: %v", err)
	}
	encoded, err := codecA.Encode(fs)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// A cookie sealed by one process's key must never decode under a
	// different key -- relevant if mikroview is ever restarted (a new
	// process, a fresh in-memory key) mid-flow: the flow should fail
	// cleanly, not partially decode.
	if _, err := codecB.Decode(encoded, 10*time.Minute, time.Now()); err != ErrFlowStateInvalid {
		t.Errorf("Decode under a different codec's key = %v, want ErrFlowStateInvalid", err)
	}
}
