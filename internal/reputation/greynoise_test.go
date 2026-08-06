package reputation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGreyNoiseClientRejectsNonPublicIP(t *testing.T) {
	c := NewGreyNoiseClient("test-key")
	for _, ip := range []string{"192.168.1.1", "10.0.0.5", "not-an-ip", ""} {
		if _, err := c.Lookup(context.Background(), ip); err != ErrNotPublic {
			t.Errorf("Lookup(%q) err = %v, want ErrNotPublic", ip, err)
		}
	}
}

func TestGreyNoiseClientNoKeyReturnsZeroResultWithoutDialing(t *testing.T) {
	// No httptest server involved at all -- an empty APIKey must never
	// dial out (see fetchGreyNoise's own key-empty short circuit). If
	// this regressed and it tried to reach the real
	// api.greynoise.io, the test would hang/fail on the sandbox's lack
	// of network access rather than passing quickly.
	c := NewGreyNoiseClient("")
	r, err := c.Lookup(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !reflect.DeepEqual(r, Result{}) {
		t.Errorf("expected a zero Result with no key configured, got %+v", r)
	}
}

// withGreyNoiseServer points greyNoiseBaseURL at srv for the duration of
// fn, restoring the real value afterward -- see greyNoiseBaseURL's own
// doc comment for why this hook exists.
func withGreyNoiseServer(t *testing.T, handler http.HandlerFunc, fn func(*GreyNoiseClient)) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	prev := greyNoiseBaseURL
	greyNoiseBaseURL = srv.URL + "/"
	defer func() { greyNoiseBaseURL = prev }()

	c := NewGreyNoiseClient("test-key")
	c.httpClient = srv.Client()
	fn(c)
}

func TestGreyNoiseClientParsesMaliciousClassification(t *testing.T) {
	withGreyNoiseServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("key"); got != "test-key" {
			t.Errorf("expected the configured API key on the request, got %q", got)
		}
		w.Write([]byte(`{"ip":"203.0.113.9","noise":true,"riot":false,"classification":"malicious","name":"Mirai"}`))
	}, func(c *GreyNoiseClient) {
		r, err := c.Lookup(context.Background(), "203.0.113.9")
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if !r.Noise || r.Riot || r.Classification != "malicious" || r.ActorName != "Mirai" {
			t.Errorf("unexpected Result: %+v", r)
		}
	})
}

func TestGreyNoiseClientParsesRiotAsBenignNoFloor(t *testing.T) {
	withGreyNoiseServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ip":"8.8.8.8","noise":false,"riot":true,"classification":"benign","name":"Google Public DNS"}`))
	}, func(c *GreyNoiseClient) {
		r, err := c.Lookup(context.Background(), "8.8.8.8")
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if !r.Riot || r.Classification != "benign" {
			t.Errorf("unexpected Result: %+v", r)
		}
		if _, ok := r.RiskFloor(); ok {
			t.Errorf("expected a benign/riot result to contribute no RiskFloor, got one")
		}
	})
}

func TestGreyNoiseClient404IsNotAnError(t *testing.T) {
	withGreyNoiseServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}, func(c *GreyNoiseClient) {
		r, err := c.Lookup(context.Background(), "203.0.113.9")
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if !reflect.DeepEqual(r, Result{}) {
			t.Errorf("expected a zero Result on 404 (no data), got %+v", r)
		}
	})
}

func TestGreyNoiseClientCachesResult(t *testing.T) {
	calls := 0
	withGreyNoiseServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"ip":"203.0.113.9","noise":true,"classification":"malicious"}`))
	}, func(c *GreyNoiseClient) {
		for i := 0; i < 3; i++ {
			if _, err := c.Lookup(context.Background(), "203.0.113.9"); err != nil {
				t.Fatalf("Lookup: %v", err)
			}
		}
		if calls != 1 {
			t.Errorf("expected exactly 1 real fetch across 3 Lookups of the same IP (rest served from cache), got %d", calls)
		}
	})
}

func TestGreyNoiseMaliciousClassificationRaisesRiskFloor(t *testing.T) {
	floor, ok := Result{Classification: "malicious"}.RiskFloor()
	if !ok || floor != GreyNoiseMaliciousFloor {
		t.Errorf("RiskFloor() = (%d, %v), want (%d, true)", floor, ok, GreyNoiseMaliciousFloor)
	}
}

func TestGreyNoiseNoiseAloneDoesNotRaiseRiskFloor(t *testing.T) {
	// Noise without a malicious classification is exactly the
	// "background scanning, not necessarily a targeted threat"
	// distinction GreyNoise exists to make -- see
	// Result.Classification's doc comment. It must not, on its own,
	// contribute a confidence floor.
	if _, ok := (Result{Noise: true}).RiskFloor(); ok {
		t.Error("expected Noise alone (no malicious classification) to contribute no RiskFloor")
	}
}
