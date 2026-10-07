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
	// StateStarting means a control operation was requested and the launcher
	// is waiting for the service to become ready. It is deliberately not
	// ONLINE and not OFFLINE: neither would be truthful yet.
	StateStarting
	// StateOnline means the server answered with a MKYBOOT-compatible response
	// and all reported diskless services are running.
	StateOnline
	// StateDegraded means the server answered correctly but at least one
	// diskless service (DHCP/TFTP/iSCSI) is not running. The web UI answers, so
	// this is NOT offline, but clients cannot PXE boot reliably.
	StateDegraded
	// StateOffline means the server could not be reached (refused/DNS).
	StateOffline
	// StateTimeout means the bounded response deadline elapsed after the TCP
	// connection had already been established - the server is reachable but did
	// not answer within the budget.
	StateTimeout
	// StateUnauthorized means HTTP 401/403 was returned.
	StateUnauthorized
	// StateServerError means an unexpected HTTP error status was returned.
	StateServerError
	// StateMalformed means the server answered but the payload was not valid
	// MKYBOOT JSON.
	StateMalformed
	// StateStopping means a stop operation was requested and the launcher is
	// waiting for the service to actually go away.
	StateStopping
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
	// dialTimeout bounds the TCP connect phase only. It stays short so a
	// closed port produces a fast, definitive OFFLINE verdict instead of a
	// long hang.
	dialTimeout = 3 * time.Second
	// statusTimeout bounds the whole status request. It has to be generous:
	// the real ?api=status handler in bin/mkyctl.lua answers only after
	//   - 3 blocking lsof calls (services.dhcp/tftp/iscsi), and
	//   - one blocking shell pipeline running tgtadm+grep per configured
	//     workstation, from mkyboot:checkstatpc (bin/mkyctl.lua:918),
	// and that blocking spawn stalls the whole nginx worker. A single shared
	// 5s budget made a healthy but loaded server report Timeout, which in turn
	// disabled every recovery action - the launcher dead-ended itself.
	statusTimeout = 20 * time.Second
	// controlTimeout and loginTimeout get the same reasoning: both are handled
	// by the same blocking Lua worker.
	controlTimeout = 20 * time.Second
	loginTimeout   = 20 * time.Second
	// statusRetryAttempts/statusRetryBackoff add bounded retries so a server
	// that is still coming up is not reported offline on the first probe.
	statusRetryAttempts = 2
	statusRetryBackoff  = 500 * time.Millisecond
	maxResponseBody     = 1 << 20 // 1 MiB hard bound on any response body
	// sessionCookieName is the fixed MKYBOOT session cookie name.
	sessionCookieName = "mkyboot_session"
)

// newTransport builds an HTTP transport with an explicit, short connect budget
// and a separate response-header budget. Proxy is disabled on purpose: LAN
// server traffic must never be routed through a system proxy, and the launcher
// must work with no internet access.
func newTransport(responseHeaderTimeout time.Duration) *http.Transport {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: time.Second,
		MaxIdleConnsPerHost:   2,
	}
}

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
			Timeout:   statusTimeout,
			Jar:       jar,
			Transport: newTransport(dialTimeout + statusTimeout),
		},
		actionClient: &http.Client{
			Timeout:   controlTimeout,
			Jar:       jar,
			Transport: newTransport(dialTimeout + controlTimeout),
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
	case StateStarting:
		return "Starting"
	case StateOnline:
		return "Online"
	case StateDegraded:
		return "Degraded"
	case StateOffline:
		return "Offline"
	case StateTimeout:
		return "Slow Response"
	case StateUnauthorized:
		return "Unauthorized"
	case StateServerError:
		return "Server Error"
	case StateMalformed:
		return "Malformed Response"
	case StateStopping:
		return "Stopping"
	default:
		return "Unknown"
	}
}

// Reachable reports whether the state means the server answered (or is at
// least reachable). Degraded is reachable: the web UI serves fine, some
// diskless services are down. Starting and Stopping are in-flight states.
func (s ServerState) Reachable() bool {
	switch s {
	case StateOnline, StateDegraded, StateUnauthorized, StateStarting, StateStopping:
		return true
	default:
		return false
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
	// DownServices names the diskless services the server reported as not
	// running. Non-empty means Degraded, not Offline.
	DownServices []string
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
//
// The dial phase and the response phase are deliberately treated differently.
// The transport uses a short connect budget and a longer response budget, so a
// dial-phase failure means "nothing is listening here" while any later failure
// means "the TCP connection was established but the server did not answer in
// time". That distinction is what keeps a busy server from being reported as
// offline and from disabling the recovery actions.
func classifyTransportError(err error) (ServerState, string) {
	// Dial-phase failure: the request never left the machine.
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		msg := opErr.Error()
		switch {
		// "connection refused" is the POSIX wording; Windows reports
		// "No connection could be made because the target machine actively
		// refused it". Matching only the former left Windows users with the
		// useless generic message.
		case strings.Contains(msg, "refused"):
			return StateOffline, "connection refused"
		case strings.Contains(msg, "no such host"):
			return StateOffline, "host not found"
		case strings.Contains(msg, "network is unreachable"),
			strings.Contains(msg, "no route to host"),
			strings.Contains(msg, "unreachable"),
			strings.Contains(msg, "unreachable network"):
			return StateOffline, "network unreachable"
		case opErr.Timeout():
			return StateOffline, "connect timed out"
		}
		return StateOffline, "cannot reach server"
	}

	// Post-dial failure: the server accepted the connection.
	if errors.Is(err, context.Canceled) {
		return StateOffline, "check cancelled"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return StateTimeout, "server reachable but slow to respond"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return StateTimeout, "server reachable but slow to respond"
	}
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "no such host"):
		return StateOffline, "host not found"
	case strings.Contains(errStr, "refused"):
		return StateOffline, "connection refused"
	case strings.Contains(errStr, "EOF"),
		strings.Contains(errStr, "reset"),
		strings.Contains(errStr, "forced close"):
		return StateOffline, "connection closed by server"
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
// GET /?api=status using a fresh background context.
func (s *MKYBootServer) CheckStatus() CheckResult {
	return s.CheckStatusContext(context.Background())
}

// CheckStatusContext performs one bounded health/status request against
// GET /?api=status. Transport problems are classified into the state taxonomy
// instead of being propagated as panics. Cancelling ctx aborts the request,
// which is how shutdown stops in-flight health checks.
func (s *MKYBootServer) CheckStatusContext(ctx context.Context) CheckResult {
	start := time.Now()
	res := CheckResult{State: StateConnecting, CheckedAt: start}

	// Honour the shorter of the package budget and the client's own budget so
	// tests (and future configuration) can tighten the bound.
	budget := statusTimeout
	if s.statusClient != nil && s.statusClient.Timeout > 0 && s.statusClient.Timeout < budget {
		budget = s.statusClient.Timeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, s.baseURL+"/?api=status", nil)
	if err != nil {
		res.State = StateOffline
		res.Detail = "cannot build request"
		res.CheckedAt = time.Now()
		return res
	}

	resp, err := s.statusClient.Do(req)
	res.Latency = time.Since(start)
	res.CheckedAt = time.Now()
	if err != nil {
		// A cancelled context means the caller is shutting down, not that the
		// server is unhealthy. Report it distinctly so the caller can ignore it.
		if ctx.Err() != nil {
			res.State = StateOffline
			res.Detail = "check cancelled"
			return res
		}
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
		res.Detail = "server reported HTTP 408"
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

	down := make([]string, 0, 3)
	if !raw.Services.DHCP {
		down = append(down, "DHCP")
	}
	if !raw.Services.TFTP {
		down = append(down, "TFTP")
	}
	if !raw.Services.ISCSI {
		down = append(down, "iSCSI")
	}

	res.Status = &ServerStatus{
		IPv4:            raw.Server.IPv4,
		Version:         raw.Server.Version,
		Vendor:          raw.Server.Vendor,
		ClientsTotal:    raw.Clients.Total,
		ClientsOnline:   raw.Clients.Online,
		ClientsOffline:  raw.Clients.Offline,
		ServicesRunning: len(down) == 0,
		DownServices:    down,
	}
	res.Detail = fmt.Sprintf("MKYBOOT %s at %s", orDash(raw.Server.Version), orDash(raw.Server.IPv4))
	if len(down) > 0 {
		// Reachable, serving the web UI, but clients cannot PXE boot reliably.
		// Reporting this as plain Online hid the only actionable signal the
		// status API provides.
		res.State = StateDegraded
		res.Detail += " - not running: " + strings.Join(down, ", ")
		return res
	}
	res.State = StateOnline
	return res
}

// CheckStatusRetry performs a status check with bounded retries and a short
// backoff, so a server that is still coming up (or briefly blocked) is not
// reported offline on the first probe.
//
// Only transient states are retried. A refusal, an HTTP error or a malformed
// payload is a deterministic answer and is returned immediately - retrying
// those only wastes time. Cancelling ctx stops immediately and returns the
// last result.
func (s *MKYBootServer) CheckStatusRetry(ctx context.Context) CheckResult {
	var res CheckResult
	for attempt := 0; attempt <= statusRetryAttempts; attempt++ {
		if ctx.Err() != nil {
			if attempt == 0 {
				res = CheckResult{State: StateOffline, Detail: "check cancelled", CheckedAt: time.Now()}
			}
			return res
		}
		res = s.CheckStatusContext(ctx)
		// Only a reachable-but-slow server benefits from another attempt, and
		// only while we have not already established a definitive verdict.
		if res.State != StateTimeout && res.State != StateConnecting {
			return res
		}
		if attempt == statusRetryAttempts {
			return res
		}
		select {
		case <-ctx.Done():
			return res
		case <-time.After(statusRetryBackoff):
		}
	}
	return res
}
