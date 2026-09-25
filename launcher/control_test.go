package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// mockMKYBoot implements the real login/status/control contracts of mkyctl.lua.
func mockMKYBoot(t *testing.T, controlCalls *int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("login") == "true":
			_ = r.ParseForm()
			if r.Form.Get("login") == "admin" && r.Form.Get("pass") == "correct-horse" {
				http.SetCookie(w, &http.Cookie{
					Name: "mkyboot_session", Value: "test-session-id", Path: "/",
					HttpOnly: true, SameSite: http.SameSiteStrictMode,
				})
				_, _ = w.Write([]byte("OK"))
			} else {
				_, _ = w.Write([]byte("ERROR: Invalid credentials"))
			}
		case r.URL.Query().Get("logout") == "true":
			_, _ = w.Write([]byte("OK"))
		case r.URL.Query().Get("api") == "status":
			c, err := r.Cookie("mkyboot_session")
			if err != nil || c.Value != "test-session-id" {
				_, _ = w.Write([]byte(`{"error":"Not authenticated"}`))
				return
			}
			_, _ = w.Write([]byte(authenticatedStatusJSON))
		case r.URL.Query().Get("api") == "server":
			c, err := r.Cookie("mkyboot_session")
			if err != nil || c.Value != "test-session-id" {
				_, _ = w.Write([]byte(`{"error":"Not authenticated"}`))
				return
			}
			atomic.AddInt32(controlCalls, 1)
			op := r.URL.Query().Get("op")
			if op != "restart" && op != "stop" {
				_, _ = w.Write([]byte(`{"error":"Invalid server operation"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"action":"` + op + `"}`))
		default:
			_, _ = w.Write([]byte(`{"error":"Not authenticated"}`))
		}
	})
	return httptest.NewServer(mux)
}

func TestLoginSuccessAndSessionUsedByControl(t *testing.T) {
	var controlCalls int32
	ts := mockMKYBoot(t, &controlCalls)
	defer ts.Close()

	s := newTestClient(t, ts.URL)
	if s.HasSession() {
		t.Fatal("no session expected before login")
	}
	if err := s.Login("correct-horse"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !s.HasSession() {
		t.Fatal("session cookie expected after login")
	}

	ack, err := s.Control(OpRestart)
	if err != nil {
		t.Fatalf("Control: %v", err)
	}
	if !ack.Accepted || ack.Action != "restart" {
		t.Errorf("ack = %+v", ack)
	}
	if atomic.LoadInt32(&controlCalls) != 1 {
		t.Errorf("control calls = %d, want 1", controlCalls)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	var controlCalls int32
	ts := mockMKYBoot(t, &controlCalls)
	defer ts.Close()

	s := newTestClient(t, ts.URL)
	err := s.Login("wrong")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	if !strings.Contains(err.Error(), "Invalid credentials") {
		t.Errorf("error = %v", err)
	}
	if s.HasSession() {
		t.Error("no session must exist after failed login")
	}
}

func TestControlRequiresAuthentication(t *testing.T) {
	var controlCalls int32
	ts := mockMKYBoot(t, &controlCalls)
	defer ts.Close()

	s := newTestClient(t, ts.URL)
	_, err := s.Control(OpStop)
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("err = %v, want ErrNotAuthenticated", err)
	}
	if atomic.LoadInt32(&controlCalls) != 0 {
		t.Error("control must not be reached without a session")
	}
}

func TestControlRejectsUnknownOperationClientSide(t *testing.T) {
	var controlCalls int32
	ts := mockMKYBoot(t, &controlCalls)
	defer ts.Close()

	s := newTestClient(t, ts.URL)
	if _, err := s.Control(ControlOp("reboot")); err == nil {
		t.Fatal("unsupported op must be rejected")
	}
	if _, err := s.Control(ControlOp("../../etc")); err == nil {
		t.Fatal("path-like op must be rejected")
	}
	if atomic.LoadInt32(&controlCalls) != 0 {
		t.Error("no request may leave the client for invalid ops")
	}
}

func TestControlInterruptedConnectionIsReported(t *testing.T) {
	var controlCalls int32
	ts := mockMKYBoot(t, &controlCalls)
	deadURL := ts.URL
	ts.Close()

	s := newTestClient(t, deadURL)
	if err := s.Login("correct-horse"); err == nil {
		t.Fatal("login against dead server must fail")
	}
	_, err := s.Control(OpRestart)
	if err == nil {
		t.Fatal("control against dead server must fail")
	}
	if !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("error = %v, want interruption wording", err)
	}
}

func TestLogoutClearsSession(t *testing.T) {
	var controlCalls int32
	ts := mockMKYBoot(t, &controlCalls)
	defer ts.Close()

	s := newTestClient(t, ts.URL)
	if err := s.Login("correct-horse"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	s.Logout()
	if s.HasSession() {
		t.Error("session must be gone after logout")
	}
	_, err := s.Control(OpRestart)
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Errorf("control after logout = %v, want ErrNotAuthenticated", err)
	}
}

func TestNewMKYBootServerValidation(t *testing.T) {
	if _, err := NewMKYBootServer("ftp://x"); !errors.Is(err, ErrInvalidURL) {
		t.Errorf("err = %v, want ErrInvalidURL", err)
	}
	s, err := NewMKYBootServer("http://127.0.0.1:8888/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.BaseURL() != "http://127.0.0.1:8888" {
		t.Errorf("BaseURL = %q", s.BaseURL())
	}
	if s.DashboardURL() != "http://127.0.0.1:8888/" {
		t.Errorf("DashboardURL = %q", s.DashboardURL())
	}
}
