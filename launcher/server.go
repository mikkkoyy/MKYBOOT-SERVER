// Package main implements the MKYBOOT Windows launcher.
//
// server.go is the dedicated server communication layer. It talks only to the
// real MKYBOOT HTTP API surface discovered in the repository:
//
//   - GET  /?api=status         server status (session required; an
//     unauthenticated "Not authenticated" JSON answer still proves the server
//     is online and serving MKYBOOT)
//   - POST /?login=true         session login (password supplied by the user
//     at runtime, held in memory only, never persisted)
//   - GET  /?api=server&op=...  authenticated server control (restart/stop)
//   - GET  /?logout=true        destroys the server-side session
//
// No credentials, cookies or tokens are ever written to disk or logged.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// ServerState is the launcher-facing server state taxonomy.
type ServerState int

const (
	// StateConnecting means a check is in flight.
	StateConnecting ServerState = iota
	// StateOnline means the server answered with a MKYBOOT-compatible response.
	StateOnline
	// StateOffline means the server could not be reached (refused/DNS).
	StateOffline
	// StateTimeout means the bounded request deadline elapsed.
	StateTimeout
	// StateUnauthorized means HTTP 401/403 was returned.
	StateUnauthorized
	// StateServerError means an unexpected HTTP error status was returned.
	StateServerError
	// StateMalformed means the server answered but the payload was not valid
	// MKYBOOT JSON.
	StateMalformed
)

// ControlOp is a supported server control operation.
type ControlOp string

const (
	// OpRestart restarts the MKYBOOT web service on the backend.
	OpRestart ControlOp = "restart"
	// OpStop stops the MKYBOOT web service on the backend.
	OpStop ControlOp = "stop"
)

// Valid reports whether the operation is part of the supported control API.
func (o ControlOp) Valid() bool {
	return o == OpRestart || o == OpStop
}

// ControlAck is the parsed response of /?api=server.
type ControlAck struct {
	Accepted bool
	Action   string
	Error    string
}

// Sentinel errors of the communication layer.
var (
	// ErrNotAuthenticated means the server requires a session for this call.
	ErrNotAuthenticated = errors.New("server session required")
	// ErrInvalidURL means the configured URL is unusable.
	ErrInvalidURL = errors.New("invalid server URL")
	// ErrUnexpectedResponse means the server answered in an unexpected shape.
	ErrUnexpectedResponse = errors.New("unexpected server response")
)

const (
	statusTimeout   = 5 * time.Second
	controlTimeout  = 8 * time.Second
	loginTimeout    = 8 * time.Second
	maxResponseBody = 1 << 20 // 1 MiB hard bound on any response body
	// sessionCookieName is the fixed MKYBOOT session cookie name.
	sessionCookieName = "mkyboot_session"
)

// MKYBootServer is the HTTP communication client for one MKYBOOT server.
// It is safe for concurrent use.
type MKYBootServer struct {
	baseURL string

	// statusClient has a short bounded timeout for health checks.
	statusClient *http.Client
	// actionClient has a slightly larger bounded timeout for login/control.
	// Both share the same in-memory cookie jar so a session established by
	// login is used by control calls. The jar lives only in process memory.
	actionClient *http.Client
	jarMu        chan struct{} // 1-buffered channel used as a mutex
}

func newJarChan() chan struct{} { return make(chan struct{}, 1) }

// NewMKYBootServer validates raw and builds a client for it.
func NewMKYBootServer(raw string) (*MKYBootServer, error) {
	norm, err := NormalizeServerURL(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cannot create cookie jar: %w", err)
	}
	s := &MKYBootServer{
		baseURL: norm,
		jarMu:   newJarChan(),
		statusClient: &http.Client{
			Timeout: statusTimeout,
			Jar:     jar,
			// Never route LAN server traffic through a system proxy.
			Transport: &http.Transport{Proxy: nil},
		},
		actionClient: &http.Client{
			Timeout:   controlTimeout,
			Jar:       jar,
			Transport: &http.Transport{Proxy: nil},
		},
	}
	return s, nil
}

// BaseURL returns the normalized server base URL (no trailing slash).
func (s *MKYBootServer) BaseURL() string { return s.baseURL }

// DashboardURL returns the URL of the MKYBOOT web UI.
func (s *MKYBootServer) DashboardURL() string { return s.baseURL + "/" }

// HasSession reports whether the in-memory jar currently holds a session
// cookie for the server. The value is never persisted.
func (s *MKYBootServer) HasSession() bool {
	u, err := url.Parse(s.baseURL)
	if err != nil {
		return false
	}
	for _, c := range s.statusClient.Jar.Cookies(u) {
		if c.Name == sessionCookieName && c.Value != "" {
			return true
		}
	}
	return false
}

// ResetSession drops all in-memory cookies without any network traffic.
// It is used when the configured server address changes.
func (s *MKYBootServer) ResetSession() {
	s.jarMu <- struct{}{}
	defer func() { <-s.jarMu }()
	jar, err := cookiejar.New(nil)
	if err != nil {
		return
	}
	s.statusClient.Jar = jar
	s.actionClient.Jar = jar
}

// Logout destroys the server-side session (best effort) and clears the jar.
func (s *MKYBootServer) Logout() {
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/?logout=true", nil)
	if err == nil {
		if resp, doErr := s.actionClient.Do(req); doErr == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
		}
	}
	s.ResetSession()
}

// rawStatus is the tolerant decode target for /?api=status.
type rawStatus struct {
	Server struct {
		IPv4    string `json:"ipv4"`
		Version string `json:"version"`
		Vendor  string `json:"vendor"`
	} `json:"server"`
	Clients struct {
		Total   int `json:"total"`
		Online  int `json:"online"`
		Offline int `json:"offline"`
	} `json:"clients"`
	Services struct {
		DHCP  bool `json:"dhcp"`
		TFTP  bool `json:"tftp"`
		ISCSI bool `json:"iscsi"`
	} `json:"services"`
	Error string `json:"error"`
}

// String returns the compact UI label for the state.
func (s ServerState) String() string {
	switch s {
	case StateConnecting:
		return "Connecting"
	case StateOnline:
		return "Online"
	case StateOffline:
		return "Offline"
	case StateTimeout:
		return "Timeout"
	case StateUnauthorized:
		return "Unauthorized"
	case StateServerError:
		return "Server Error"
	case StateMalformed:
		return "Malformed Response"
	default:
		return "Unknown"
	}
}

// ServerStatus is the parsed authenticated /?api=status payload.
type ServerStatus struct {
	IPv4            string
	Version         string
	Vendor          string
	ClientsTotal    int
	ClientsOnline   int
	ClientsOffline  int
	ServicesRunning bool
}

// CheckResult is the outcome of one status/health check.
type CheckResult struct {
	State        ServerState
	Detail       string
	AuthRequired bool
	Status       *ServerStatus
	CheckedAt    time.Time
	Latency      time.Duration
}

// Login exchanges the runtime-supplied password for a server session cookie.
// The password is only held for the duration of this call; it is never stored
// or logged. The server answers plain text "OK" or "ERROR: ...".
func (s *MKYBootServer) Login(password string) error {
	form := url.Values{}
	form.Set("login", "admin") // username is fixed server-side by MKYBOOT
	form.Set("pass", password)

	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/?login=true",
		strings.NewReader(form.Encode()))
	if err != nil {
		return ErrInvalidURL
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.actionClient.Do(req)
	if err != nil {
		state, detail := classifyTransportError(err)
		return fmt.Errorf("login failed: %s (%s)", state, detail)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return errors.New("login failed: cannot read response")
	}
	reply := strings.TrimSpace(string(body))
	switch {
	case reply == "OK":
		if !s.HasSession() {
			return errors.New("login response OK but no session cookie received")
		}
		return nil
	case strings.HasPrefix(reply, "ERROR"):
		return errors.New(sanitizeDetail(reply))
	default:
		return ErrUnexpectedResponse
	}
}

// classifyTransportError maps a low-level HTTP error to state+detail without
// leaking URL credentials (URLs are already credential-free by validation).
func classifyTransportError(err error) (ServerState, string) {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return StateTimeout, "request timed out"
	}
	errStr := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return StateTimeout, "request timed out"
	case strings.Contains(errStr, "connection refused"):
		return StateOffline, "connection refused"
	case strings.Contains(errStr, "no such host"):
		return StateOffline, "host not found"
	case strings.Contains(errStr, "DNS"),
		strings.Contains(errStr, "network is unreachable"),
		strings.Contains(errStr, "server gave HTTP response"),
		strings.Contains(errStr, "EOF"),
		strings.Contains(errStr, "reset"),
		strings.Contains(errStr, "refused"),
		strings.Contains(errStr, "forced close"):
		return StateOffline, "connection failed"
	}
	return StateOffline, "cannot reach server"
}

// sanitizeDetail bounds and strips control characters from server-provided
// text before it is displayed or attached to errors.
func sanitizeDetail(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) > 160 {
		s = s[:160] + "..."
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Control requests a server control operation using the live session
// (POST-established cookie). Only the two hard-coded supported operations are
// ever sent; op is validated client-side and no other user-controlled data
// reaches the request.
func (s *MKYBootServer) Control(op ControlOp) (ControlAck, error) {
	if !op.Valid() {
		return ControlAck{}, fmt.Errorf("unsupported control operation %q", string(op))
	}
	// Re-validate the base URL on every request (defense in depth).
	u, err := NormalizeServerURL(s.baseURL)
	if err != nil {
		return ControlAck{}, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	target := u + "/?api=server&op=" + url.QueryEscape(string(op))

	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return ControlAck{}, ErrInvalidURL
	}

	resp, err := s.actionClient.Do(req)
	if err != nil {
		// Restarting/stopping the web service often kills the very connection
		// carrying this request; callers must verify the effect via status
		// polling instead of trusting this error.
		return ControlAck{}, fmt.Errorf("control request interrupted: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return ControlAck{}, errors.New("cannot read control response")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ControlAck{}, fmt.Errorf("%w (HTTP %d)", ErrNotAuthenticated, resp.StatusCode)
	}

	var payload struct {
		Success *bool  `json:"success"`
		Action  string `json:"action"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ControlAck{}, ErrUnexpectedResponse
	}
	if payload.Error != "" {
		if strings.EqualFold(payload.Error, "Not authenticated") {
			return ControlAck{}, ErrNotAuthenticated
		}
		return ControlAck{}, errors.New(sanitizeDetail(payload.Error))
	}
	if payload.Success == nil {
		return ControlAck{}, ErrUnexpectedResponse
	}
	ack := ControlAck{Accepted: *payload.Success, Action: payload.Action, Error: payload.Error}
	if !ack.Accepted {
		return ack, errors.New("server rejected operation: " + sanitizeDetail(ack.Error))
	}
	return ack, nil
}

// CheckStatus performs one bounded health/status request against
// GET /?api=status. Transport problems are classified into the state taxonomy
// instead of being propagated as panics.
func (s *MKYBootServer) CheckStatus() CheckResult {
	start := time.Now()
	res := CheckResult{State: StateConnecting, CheckedAt: start}

	ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/?api=status", nil)
	if err != nil {
		res.State = StateOffline
		res.Detail = "cannot build request"
		return res
	}

	resp, err := s.statusClient.Do(req)
	res.Latency = time.Since(start)
	res.CheckedAt = time.Now()
	if err != nil {
		res.State, res.Detail = classifyTransportError(err)
		return res
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		res.State = StateServerError
		res.Detail = "failed to read response"
		return res
	}
	if len(body) > maxResponseBody {
		res.State = StateMalformed
		res.Detail = "response too large"
		return res
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		res.State = StateUnauthorized
		res.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return res
	case resp.StatusCode == http.StatusRequestTimeout:
		res.State = StateTimeout
		res.Detail = "HTTP 408"
		return res
	case resp.StatusCode >= 400:
		// 404 and other 4xx/5xx: reachable but not the expected API surface.
		res.State = StateServerError
		res.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return res
	}

	var raw rawStatus
	if err := json.Unmarshal(body, &raw); err != nil {
		res.State = StateMalformed
		res.Detail = "invalid JSON payload"
		return res
	}

	// Unauthenticated answer: server is reachable and MKYBOOT is serving.
	if raw.Error != "" {
		if strings.EqualFold(raw.Error, "Not authenticated") {
			res.State = StateOnline
			res.AuthRequired = true
			res.Detail = "Reachable (dashboard sign-in required)"
			return res
		}
		res.State = StateMalformed
		res.Detail = "API error: " + sanitizeDetail(raw.Error)
		return res
	}

	// Authenticated status payload: require MKYBOOT marker fields so a random
	// JSON endpoint cannot masquerade as the server.
	if raw.Server.IPv4 == "" && raw.Server.Version == "" && raw.Clients.Total == 0 {
		res.State = StateMalformed
		res.Detail = "payload missing MKYBOOT status fields"
		return res
	}
	res.State = StateOnline
	res.Detail = fmt.Sprintf("MKYBOOT %s at %s", orDash(raw.Server.Version), orDash(raw.Server.IPv4))
	res.Status = &ServerStatus{
		IPv4:            raw.Server.IPv4,
		Version:         raw.Server.Version,
		Vendor:          raw.Server.Vendor,
		ClientsTotal:    raw.Clients.Total,
		ClientsOnline:   raw.Clients.Online,
		ClientsOffline:  raw.Clients.Offline,
		ServicesRunning: raw.Services.DHCP && raw.Services.TFTP && raw.Services.ISCSI,
	}
	return res
}
