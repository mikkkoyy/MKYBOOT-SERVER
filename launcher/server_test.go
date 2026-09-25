package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// authenticatedStatusJSON matches the real mkyctl.lua get_server_status shape.
const authenticatedStatusJSON = `{"server":{"ipv4":"192.168.0.2","version":"4.0.0","vendor":"Nuke Technology LLC"},"clients":{"total":3,"online":1,"offline":2},"iscsi":{"port":3260,"iqn":"2020-02-10.com.mkyboot"},"images":[],"services":{"dhcp":true,"tftp":true,"iscsi":true}}`

// newTestClient wires a MKYBootServer to the given test server URL.
func newTestClient(t *testing.T, baseURL string) *MKYBootServer {
	t.Helper()
	s, err := NewMKYBootServer(baseURL)
	if err != nil {
		t.Fatalf("NewMKYBootServer(%q): %v", baseURL, err)
	}
	return s
}

func TestStatusAuthenticatedPayload(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || r.URL.Query().Get("api") != "status" {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(authenticatedStatusJSON))
	}))
	defer ts.Close()

	s := newTestClient(t, ts.URL)
	res := s.CheckStatus()
	if res.State != StateOnline {
		t.Fatalf("state = %v (%s), want Online", res.State, res.Detail)
	}
	if res.AuthRequired {
		t.Error("authenticated payload must not set AuthRequired")
	}
	if res.Status == nil {
		t.Fatal("expected parsed status")
	}
	if res.Status.Version != "4.0.0" || res.Status.IPv4 != "192.168.0.2" {
		t.Errorf("parsed status = %+v", res.Status)
	}
	if res.Status.ClientsTotal != 3 || res.Status.ClientsOnline != 1 {
		t.Errorf("clients parsed wrong: %+v", res.Status)
	}
	if !res.Status.ServicesRunning {
		t.Error("services should be running")
	}
	// On VMs with coarse timer resolution a very fast loopback round trip
	// can measure as 0; only reject invalid (unset/negative) values.
	if res.Latency < 0 {
		t.Errorf("latency invalid: %v", res.Latency)
	}
	if res.CheckedAt.IsZero() {
		t.Error("checkedAt must be set")
	}
}

func TestStatusUnauthenticatedMeansOnline(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"Not authenticated"}`))
	}))
	defer ts.Close()

	res := newTestClient(t, ts.URL).CheckStatus()
	if res.State != StateOnline {
		t.Fatalf("state = %v, want Online", res.State)
	}
	if !res.AuthRequired {
		t.Error("auth-required flag missing")
	}
	if res.Status != nil {
		t.Error("no status payload expected")
	}
}

func TestStatusOffline(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := ts.URL
	ts.Close() // nothing listens anymore

	res := newTestClient(t, deadURL).CheckStatus()
	if res.State != StateOffline {
		t.Fatalf("state = %v (%s), want Offline", res.State, res.Detail)
	}
}

func TestStatusTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer ts.Close()

	s := newTestClient(t, ts.URL)
	s.statusClient.Timeout = 200 * time.Millisecond // shrink bound for the test
	res := s.CheckStatus()
	if res.State != StateTimeout {
		t.Fatalf("state = %v (%s), want Timeout", res.State, res.Detail)
	}
}

func TestStatusHTTPErrorCodes(t *testing.T) {
	cases := []struct {
		code int
		want ServerState
	}{
		{http.StatusUnauthorized, StateUnauthorized},
		{http.StatusForbidden, StateUnauthorized},
		{http.StatusNotFound, StateServerError},
		{http.StatusInternalServerError, StateServerError},
		{http.StatusBadGateway, StateServerError},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(http.StatusText(tc.code), func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
			}))
			defer ts.Close()
			res := newTestClient(t, ts.URL).CheckStatus()
			if res.State != tc.want {
				t.Errorf("HTTP %d -> state %v (%s), want %v", tc.code, res.State, res.Detail, tc.want)
			}
		})
	}
}

func TestStatusMalformedResponses(t *testing.T) {
	bodies := []string{
		`<html>not json</html>`,
		`{`,
		`{"foo":1}`,
		`[]`,
		`{"error":"something odd"}`,
	}
	for _, body := range bodies {
		body := body
		t.Run(body, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer ts.Close()
			res := newTestClient(t, ts.URL).CheckStatus()
			if res.State != StateMalformed {
				t.Errorf("body %q -> state %v, want Malformed Response", body, res.State)
			}
		})
	}
}
