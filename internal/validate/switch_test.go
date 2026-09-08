package validate_test

import (
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/validate"
)

func TestSwitchBridgeVLANsAndAdminUpPortsMatchBaseline(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "switch", "convergence_timeout: 1ms\n")
	client := switchClient(t).
		set(validate.CmdBridge, ros6Fixture(t, "bridge-print.txt")).
		set(validate.CmdBridgeVLAN, ros6Fixture(t, "bridge-vlan-print.txt")).
		set(validate.CmdInterface, ros6Fixture(t, "interface-print.txt"))
	writeCapturedBaseline(t, cfg, client, "switch")

	if err := runCheck(t, cfg, testDeviceRole("switch"), client, newFakeClock()); err != nil {
		t.Fatal(err)
	}
	assertROS6Commands(t, client.runs, validate.CmdBridge, validate.CmdBridgeVLAN, validate.CmdInterface)
}

func TestSwitchBridgeDownFails(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "switch", "convergence_timeout: 1ms\n")
	client := switchClient(t).
		set(validate.CmdBridge, ros6Fixture(t, "bridge-print.txt")).
		set(validate.CmdBridgeVLAN, ros6Fixture(t, "bridge-vlan-print.txt")).
		set(validate.CmdInterface, ros6Fixture(t, "interface-print.txt"))
	writeCapturedBaseline(t, cfg, client, "switch")
	client.set(validate.CmdBridge, ros6Fixture(t, "bridge-print-down.txt"))

	err := runCheck(t, cfg, testDeviceRole("switch"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected bridge down")
	}
	if !strings.Contains(err.Error(), "bridge1") {
		t.Fatalf("got %v", err)
	}
}

func TestSwitchVLANMismatchFails(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "switch", "convergence_timeout: 1ms\n")
	client := switchClient(t).
		set(validate.CmdBridge, ros6Fixture(t, "bridge-print.txt")).
		set(validate.CmdBridgeVLAN, ros6Fixture(t, "bridge-vlan-print.txt")).
		set(validate.CmdInterface, ros6Fixture(t, "interface-print.txt"))
	writeCapturedBaseline(t, cfg, client, "switch")
	client.set(validate.CmdBridgeVLAN, ros6Fixture(t, "bridge-vlan-print-changed.txt"))

	err := runCheck(t, cfg, testDeviceRole("switch"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected vlan mismatch")
	}
	if !strings.Contains(err.Error(), "20") && !strings.Contains(err.Error(), "vlan") {
		t.Fatalf("got %v", err)
	}
}

func TestSwitchAdminUpPortDisabledFails(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "switch", "convergence_timeout: 1ms\n")
	client := switchClient(t).
		set(validate.CmdBridge, ros6Fixture(t, "bridge-print.txt")).
		set(validate.CmdBridgeVLAN, ros6Fixture(t, "bridge-vlan-print.txt")).
		set(validate.CmdInterface, ros6Fixture(t, "interface-print.txt"))
	writeCapturedBaseline(t, cfg, client, "switch")
	client.set(validate.CmdInterface, ros6Fixture(t, "interface-print-port-down.txt"))

	err := runCheck(t, cfg, testDeviceRole("switch"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected admin-up port disabled")
	}
	if !strings.Contains(err.Error(), "ether2") {
		t.Fatalf("got %v", err)
	}
}

func switchClient(t *testing.T) *fakeClient {
	t.Helper()
	return newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt"))
}
