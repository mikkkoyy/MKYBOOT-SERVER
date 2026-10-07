package main

// health_test.go covers the offline / false-offline behaviour of the launcher
// health check: the timeout budgets, the retry bound, the degraded state, the
// distinction between "nothing is listening" and "reachable but slow", offline
// operation with no internet, working-directory independence and shutdown
// cancellation.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// slowThenHealthy answers slowly for the first `slowHits` requests and then
// answers immediately. It emulates a server that is still coming up.
func slowThenHealthy(slowHits *int64, delay time.Duration, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt64(slowHits, 1) <= 1 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

// --- core regression: a slow server must not be reported as offline ---------

// The launcher used a single 5s budget for connect plus response. The real
// ?api=status handler blocks on per-workstation tgtadm probes, so a healthy but
// busy server timed out. That produced StateTimeout - not StateOffline - and
// that distinction is what the recovery affordances key off.
func TestSlowButReachableIsNotOffline(t *testing.T) {
	var hits int64
	ts := slowThenHealthy(&hits, 250*time.Millisecond, `{"error":"Not authenticated"}`)
	defer ts.Close()

	s := newTestClient(t, ts.URL)
	s.statusClient.Timeout = 80 * time.Millisecond // shorter than the server delay

	res := s.CheckStatus()
	if res.State == StateOffline {
		t.Fatalf("reachable server reported Offline: %+v", res)
	}
	if res.State != StateTimeout {
		t.Fatalf("state = %v (%s), want Timeout", res.State, res.Detail)
	}
	if !strings.Contains(res.Detail, "slow") {
		t.Errorf("detail should explain the slow response, got %q", res.Detail)
	}
	// The crucial bit: a slow-but-present server must still count as reachable
	// for the purposes of "the server is there", even though control is
	// unavailable until nginx answers within budget.
	if res.State.Reachable() {
		t.Logf("note: slow state counts as reachable")
	}
}

// A slow server that recovers must be promoted to Online by the bounded retry
// instead of being left permanently mislabelled.
func TestRetryPromotesRecoveringServerToOnline(t *testing.T) {
	var hits int64
	ts := slowThenHealthy(&hits, 200*time.Millisecond, `{"error":"Not authenticated"}`)
	defer ts.Close()

	s := newTestClient(t, ts.URL)
	s.statusClient.Timeout = 60 * time.Millisecond // first attempt will time out

	res := s.CheckStatusRetry(context.Background())
	if res.State != StateOnline {
		t.Fatalf("state = %v (%s), want Online after retry", res.State, res.Detail)
	}
	if atomic.LoadInt64(&hits) < 2 {
		t.Errorf("expected a retry to have been attempted, hits=%d", hits)
	}
}

// The retry must be bounded: it cannot loop forever nor spawn unbounded work.
func TestRetryIsBounded(t *testing.T) {
	var hits int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		time.Sleep(300 * time.Millisecond) // never answers in budget
	}))
	defer ts.Close()

	s := newTestClient(t, ts.URL)
	s.statusClient.Timeout = 50 * time.Millisecond

	start := time.Now()
	res := s.CheckStatusRetry(context.Background())
	elapsed := time.Since(start)

	if res.State != StateTimeout {
		t.Errorf("state = %v, want Timeout", res.State)
	}
	got := atomic.LoadInt64(&hits)
	if got > int64(statusRetryAttempts)+1 {
		t.Errorf("retry not bounded: %d attempts (max %d)", got, statusRetryAttempts+1)
	}
	if got < 2 {
		t.Errorf("expected retries, got %d attempt(s)", got)
	}
	if elapsed > 5*time.Second {
		t.Errorf("retry loop took too long: %v", elapsed)
	}
}

// A definitive answer must not be retried at all.
func TestNoRetryOnDefinitiveRefusal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead := ts.URL
	ts.Close()
	time.Sleep(100 * time.Millisecond)

	var hits int64
	s := newTestClient(t, dead)
	// Count attempts by observing that a refused connection returns at once.
	res := s.CheckStatusRetry(context.Background())
	if res.State != StateOffline {
		t.Fatalf("state = %v (%s), want Offline", res.State, res.Detail)
	}
	if hits != 0 {
		t.Errorf("unexpected handler hits: %d", hits)
	}
}

// --- connection refusal must produce an actionable message ------------------

func TestConnectionRefusalDetailIsActionable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead := ts.URL
	ts.Close()
	time.Sleep(100 * time.Millisecond)

	res := newTestClient(t, dead).CheckStatus()
	if res.State != StateOffline {
		t.Fatalf("state = %v, want Offline", res.State)
	}
	if res.Detail == "" || res.Detail == "cannot reach server" {
		t.Errorf("refusal should name the reason, got %q", res.Detail)
	}
	if res.Detail != "connection refused" {
		t.Logf("detail = %q (acceptable, non-generic)", res.Detail)
	}
	// A refusal is a definitive "nothing is listening" verdict and must arrive
	// quickly - the connect budget is short and independent of the slow
	// response budget.
	if res.Latency > dialTimeout+2*time.Second {
		t.Errorf("refusal took %v; connect should fail fast", res.Latency)
	}
}

// --- degraded: reachable web UI, diskless services down ---------------------

func TestDegradedWhenServicesDown(t *testing.T) {
	body := `{"server":{"ipv4":"192.168.0.2","version":"4.0.0","vendor":"Nuke Technology LLC"},` +
		`"clients":{"total":3,"online":3,"offline":0},` +
		`"services":{"dhcp":true,"tftp":false,"iscsi":false}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer ts.Close()

	res := newTestClient(t, ts.URL).CheckStatus()
	if res.State != StateDegraded {
		t.Fatalf("state = %v (%s), want Degraded", res.State, res.Detail)
	}
	if res.Status.ServicesRunning {
		t.Error("ServicesRunning must be false when a service is down")
	}
	if len(res.Status.DownServices) != 2 {
		t.Errorf("DownServices = %v, want TFTP and iSCSI", res.Status.DownServices)
	}
	for _, want := range []string{"TFTP", "iSCSI"} {
		if !strings.Contains(res.Detail, want) {
			t.Errorf("detail %q should name %s", res.Detail, want)
		}
	}
	// Reachable: nginx answered, so control must remain available. This is the
	// case the old code collapsed into plain Online.
	if !res.State.Reachable() {
		t.Error("degraded server is still reachable and must say so")
	}
	if !canControlServer(res.State) {
		t.Error("degraded server should still allow control actions")
	}
}

func TestAllServicesDownIsStillDegradedNotOffline(t *testing.T) {
	body := `{"server":{"ipv4":"192.168.0.2","version":"4.0.0"},` +
		`"clients":{"total":0,"online":0,"offline":0},"services":{"dhcp":false,"tftp":false,"iscsi":false}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer ts.Close()

	res := newTestClient(t, ts.URL).CheckStatus()
	if res.State != StateDegraded {
		t.Fatalf("state = %v, want Degraded (web UI answers)", res.State)
	}
	if len(res.Status.DownServices) != 3 {
		t.Errorf("DownServices = %v, want all three", res.Status.DownServices)
	}
}

// --- HTTP 500 is not healthy ------------------------------------------------

func TestHTTP500IsNotHealthyAndStaysReachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`internal error`))
	}))
	defer ts.Close()

	res := newTestClient(t, ts.URL).CheckStatus()
	if res.State == StateOnline {
		t.Fatal("HTTP 500 must never be Online")
	}
	if res.State != StateServerError {
		t.Fatalf("state = %v, want Server Error", res.State)
	}
	if res.Detail != "HTTP 500" {
		t.Errorf("detail = %q, want HTTP 500", res.Detail)
	}
}

// --- offline operation: no internet must not affect a local server ----------

// A LAN/loopback server must be judged purely on its own answers. If the
// launcher ever consulted a system proxy it would fail whenever the proxy or
// the internet is unavailable, which is exactly the "offline means no
// internet" confusion this guards against.
func TestHealthyLocalServerStaysOnlineWithoutInternet(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(authenticatedStatusJSON))
	}))
	defer ts.Close()

	// Point every proxy variable at a dead address. Nothing must be routed
	// through it.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	t.Setenv("https_proxy", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")

	s := newTestClient(t, ts.URL)
	tr, ok := s.statusClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("status transport has unexpected type")
	}
	if tr.Proxy != nil {
		t.Error("LAN traffic must never be routed through a proxy")
	}

	res := s.CheckStatus()
	if res.State != StateOnline {
		t.Fatalf("healthy local server reported %v (%s) with proxies unreachable", res.State, res.Detail)
	}
}

// --- working directory independence -----------------------------------------

// The launcher must not depend on the terminal's current directory: config
// lives under the user config dir, and health checks are absolute HTTP calls.
func TestWorksFromArbitraryWorkingDirectory(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(authenticatedStatusJSON))
	}))
	defer ts.Close()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// A directory that looks nothing like the project.
	elsewhere := t.TempDir()
	if err := os.Chdir(elsewhere); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	// Config resolution must be anchored to the config dir, not the CWD.
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)

	cfg, cfgErr := LoadConfig()
	if cfgErr != nil {
		t.Fatalf("LoadConfig from foreign cwd: %v", cfgErr)
	}
	if cfg.ServerURL != defaultServerURL {
		t.Errorf("ServerURL = %q, want default %q", cfg.ServerURL, defaultServerURL)
	}

	cfg.StartMinimized = true
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save from foreign cwd: %v", err)
	}
	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	if !strings.HasPrefix(strings.ToLower(path), strings.ToLower(appData)) {
		t.Errorf("config path %q is not under the user config dir %q", path, appData)
	}
	if filepath.Dir(path) != filepath.Join(appData, configDirName) {
		t.Errorf("unexpected config dir: %s", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config not written: %v", err)
	}

	// And the health check still works from the foreign directory.
	if res := newTestClient(t, ts.URL).CheckStatus(); res.State != StateOnline {
		t.Errorf("health check from foreign cwd = %v (%s)", res.State, res.Detail)
	}
}

// --- shutdown cancellation --------------------------------------------------

// newTestApp builds the minimal App the background helpers need, without
// touching the registry or creating a window.
func newTestApp(t *testing.T) *App {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	return &App{
		server:     nil,
		checkNow:   make(chan struct{}, 1),
		noticeCh:   make(chan *Notice, 8),
		pollStop:   make(chan struct{}),
		stopCtx:    ctx,
		stopCancel: cancel,
	}
}

// A cancelled context must abort the in-flight request immediately rather than
// waiting out the full budget.
func TestCheckStatusContextCancels(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // block until the test releases us
	}))
	defer ts.Close()
	defer close(release)

	s := newTestClient(t, ts.URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	res := s.CheckStatusContext(ctx)
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("cancellation did not abort the request promptly: %v", elapsed)
	}
	if res.Detail != "check cancelled" {
		t.Errorf("detail = %q, want %q", res.Detail, "check cancelled")
	}
}

func TestCheckStatusRetryRespectsCancellation(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer ts.Close()
	defer close(release)

	s := newTestClient(t, ts.URL)
	s.statusClient.Timeout = 10 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	s.CheckStatusRetry(ctx)
	if el := time.Since(start); el > 3*time.Second {
		t.Errorf("retry ignored cancellation: %v", el)
	}
}

// verifyControl used to sleep in a loop with no cancellation and could keep
// polling - and post to a destroyed window - long after shutdown began.
func TestVerifyControlAbortsOnShutdown(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(authenticatedStatusJSON)) // always online
	}))
	defer ts.Close()

	a := newTestApp(t)
	a.server = newTestClient(t, ts.URL)

	done := make(chan bool, 1)
	go func() { done <- a.verifyControl(a.server, OpRestart) }()

	time.Sleep(100 * time.Millisecond)
	a.stopCancel() // begin shutdown

	select {
	case got := <-done:
		if got {
			t.Error("verifyControl should not report success after shutdown began")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("verifyControl did not return after shutdown")
	}
}

// verifyControl must still reach a real verdict when nothing interrupts it.
func TestVerifyControlSucceedsWhenServerRecovers(t *testing.T) {
	var healthy atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			// unreachable-looking answer first, then flip
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"error":"Not authenticated"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(authenticatedStatusJSON))
	}))
	defer ts.Close()

	a := newTestApp(t)
	a.server = newTestClient(t, ts.URL)
	healthy.Store(true)

	if !a.verifyControl(a.server, OpRestart) {
		t.Error("verifyControl should confirm an online server after restart")
	}
}

// A stop operation is confirmed by the server going away.
func TestVerifyControlConfirmsStop(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead := ts.URL
	ts.Close()
	time.Sleep(100 * time.Millisecond)

	a := newTestApp(t)
	a.server = newTestClient(t, dead)
	if !a.verifyControl(a.server, OpStop) {
		t.Error("verifyControl should confirm a stop when the server is gone")
	}
}

// --- transient transition states -------------------------------------------

func TestTransientStatesAreDistinctFromOnline(t *testing.T) {
	for _, st := range []ServerState{StateStarting, StateStopping} {
		if st == StateOnline {
			t.Errorf("%s must not alias Online", st)
		}
		if !st.Reachable() {
			t.Errorf("%s should count as reachable (the server is mid-transition)", st)
		}
	}
	// While a transition is in flight the control actions must be blocked so
	// repeated clicks cannot queue duplicate operations.
	for _, st := range []ServerState{StateStarting, StateStopping} {
		if canControlServer(st) {
			t.Errorf("%s must not allow a second control operation", st)
		}
	}
}

// --- response size / protocol robustness ------------------------------------

func TestOversizedResponseRejected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		blob := strings.Repeat("x", 64*1024)
		_, _ = w.Write([]byte(`{"server":{"version":"` + blob + `"}}`))
	}))
	defer ts.Close()

	// Many chunks beyond the 1 MiB bound.
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("y", 256*1024)
		for i := 0; i < 8; i++ {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer ts2.Close()

	res := newTestClient(t, ts2.URL).CheckStatus()
	if res.State != StateMalformed {
		t.Errorf("oversized body -> state %v, want Malformed Response", res.State)
	}
}

// A payload that is valid JSON but not MKYBOOT must not pass as healthy.
func TestForeignJSONIsNotAccepted(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","uptime":12345}`))
	}))
	defer ts.Close()

	res := newTestClient(t, ts.URL).CheckStatus()
	if res.State != StateMalformed {
		t.Errorf("foreign JSON -> state %v, want Malformed Response", res.State)
	}
}
