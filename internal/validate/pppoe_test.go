package validate_test

import (
	"strings"
	"testing"
	"time"

	"github.com/taihen/rosup/internal/validate"
)

func pppoeProfileYAML() string {
	return `convergence_timeout: 1ms
session_restore_timeout: 10m
`
}

func TestPPPoEServerAndAAAMatchBaseline(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "pppoe", pppoeProfileYAML())
	client := pppoeClient(t).
		set(validate.CmdPPPoEServer, ros6Fixture(t, "pppoe-server-print.txt")).
		set(validate.CmdPPPAAA, ros6Fixture(t, "ppp-aaa-print.txt")).
		set(validate.CmdRADIUS, ros6Fixture(t, "radius-print.txt")).
		set(validate.CmdPPPActive, ros6Fixture(t, "ppp-active-print.txt"))
	writeCapturedBaseline(t, cfg, client, "pppoe")

	if err := runCheck(t, cfg, testDeviceRole("pppoe"), client, newFakeClock()); err != nil {
		t.Fatal(err)
	}
	assertROS6Commands(t, client.runs, validate.CmdPPPoEServer, validate.CmdPPPAAA, validate.CmdRADIUS, validate.CmdPPPActive)
}

func TestPPPoEServerDisabledFails(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "pppoe", pppoeProfileYAML())
	client := pppoeClient(t).
		set(validate.CmdPPPoEServer, ros6Fixture(t, "pppoe-server-print.txt")).
		set(validate.CmdPPPAAA, ros6Fixture(t, "ppp-aaa-print.txt")).
		set(validate.CmdRADIUS, ros6Fixture(t, "radius-print.txt")).
		set(validate.CmdPPPActive, ros6Fixture(t, "ppp-active-print.txt"))
	writeCapturedBaseline(t, cfg, client, "pppoe")
	client.set(validate.CmdPPPoEServer, ros6Fixture(t, "pppoe-server-print-disabled.txt"))

	err := runCheck(t, cfg, testDeviceRole("pppoe"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected disabled server")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "pppoe") && !strings.Contains(strings.ToLower(err.Error()), "server") {
		t.Fatalf("got %v", err)
	}
}

func TestPPPoEAAARadiusMustMatchBaseline(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "pppoe", pppoeProfileYAML())
	client := pppoeClient(t).
		set(validate.CmdPPPoEServer, ros6Fixture(t, "pppoe-server-print.txt")).
		set(validate.CmdPPPAAA, ros6Fixture(t, "ppp-aaa-print.txt")).
		set(validate.CmdRADIUS, ros6Fixture(t, "radius-print.txt")).
		set(validate.CmdPPPActive, ros6Fixture(t, "ppp-active-print.txt"))
	writeCapturedBaseline(t, cfg, client, "pppoe")
	client.set(validate.CmdPPPAAA, ros6Fixture(t, "ppp-aaa-print-noradius.txt"))

	err := runCheck(t, cfg, testDeviceRole("pppoe"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected AAA mismatch")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "radius") && !strings.Contains(strings.ToLower(err.Error()), "aaa") {
		t.Fatalf("got %v", err)
	}
}

func TestPPPoESessionsWaitRestoreTimeoutBeforeFail(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "pppoe", pppoeProfileYAML())
	client := pppoeClient(t).
		set(validate.CmdPPPoEServer, ros6Fixture(t, "pppoe-server-print.txt")).
		set(validate.CmdPPPAAA, ros6Fixture(t, "ppp-aaa-print.txt")).
		set(validate.CmdRADIUS, ros6Fixture(t, "radius-print.txt")).
		set(validate.CmdPPPActive, ros6Fixture(t, "ppp-active-print.txt"))
	writeCapturedBaseline(t, cfg, client, "pppoe")
	client.setSeq(validate.CmdPPPActive, ros6Fixture(t, "ppp-active-print-empty.txt"), ros6Fixture(t, "ppp-active-print-empty.txt"))

	clock := newFakeClock()
	err := runCheck(t, cfg, testDeviceRole("pppoe"), client, clock)
	if err == nil {
		t.Fatal("expected missing sessions")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "session") {
		t.Fatalf("got %v", err)
	}
	if !sleptAtLeast(clock, 10*time.Minute) {
		t.Fatalf("sleeps %v, want session_restore_timeout 10m before fail", clock.sleeps)
	}
}

func TestPPPoESessionsRestoreWithinTimeout(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "pppoe", pppoeProfileYAML())
	client := pppoeClient(t).
		set(validate.CmdPPPoEServer, ros6Fixture(t, "pppoe-server-print.txt")).
		set(validate.CmdPPPAAA, ros6Fixture(t, "ppp-aaa-print.txt")).
		set(validate.CmdRADIUS, ros6Fixture(t, "radius-print.txt")).
		set(validate.CmdPPPActive, ros6Fixture(t, "ppp-active-print.txt"))
	writeCapturedBaseline(t, cfg, client, "pppoe")
	client.setSeq(validate.CmdPPPActive, ros6Fixture(t, "ppp-active-print-empty.txt"), ros6Fixture(t, "ppp-active-print.txt"))

	clock := newFakeClock()
	if err := runCheck(t, cfg, testDeviceRole("pppoe"), client, clock); err != nil {
		t.Fatal(err)
	}
	if !sleptAtLeast(clock, 10*time.Minute) {
		t.Fatalf("sleeps %v, want session_restore_timeout 10m", clock.sleeps)
	}
}

func TestLoadProfilePPPoESessionRestoreTimeout(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "pppoe", pppoeProfileYAML())
	p, err := validate.LoadProfile(cfg, "pppoe")
	if err != nil {
		t.Fatal(err)
	}
	if p.SessionRestoreTimeout != 10*time.Minute {
		t.Fatalf("timeout %s", p.SessionRestoreTimeout)
	}
}

func pppoeClient(t *testing.T) *fakeClient {
	t.Helper()
	return newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt"))
}

func sleptAtLeast(clock *fakeClock, d time.Duration) bool {
	var total time.Duration
	for _, s := range clock.sleeps {
		total += s
		if s >= d {
			return true
		}
	}
	return total >= d
}
