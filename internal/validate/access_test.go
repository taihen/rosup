package validate_test

import (
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/validate"
)

func TestAccessMatchesInterfacesBridgeVLANsRoutesAndManagement(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "access", "convergence_timeout: 1ms\n")
	client := accessClient(t).
		set(validate.CmdInterface, ros6Fixture(t, "interface-print.txt")).
		set(validate.CmdBridge, ros6Fixture(t, "bridge-print.txt")).
		set(validate.CmdBridgeVLAN, ros6Fixture(t, "bridge-vlan-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt")).
		set(validate.CmdIPAddress, ros6Fixture(t, "ip-address-print.txt"))
	writeCapturedBaseline(t, cfg, client, "access")

	if err := runCheck(t, cfg, testDeviceRole("access"), client, newFakeClock()); err != nil {
		t.Fatal(err)
	}
	assertROS6Commands(t, client.runs,
		validate.CmdInterface, validate.CmdBridge, validate.CmdBridgeVLAN,
		validate.CmdIPRoute, validate.CmdIPAddress)
}

func TestAccessMissingRouteFails(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "access", "convergence_timeout: 1ms\n")
	client := accessClient(t).
		set(validate.CmdInterface, ros6Fixture(t, "interface-print.txt")).
		set(validate.CmdBridge, ros6Fixture(t, "bridge-print.txt")).
		set(validate.CmdBridgeVLAN, ros6Fixture(t, "bridge-vlan-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt")).
		set(validate.CmdIPAddress, ros6Fixture(t, "ip-address-print.txt"))
	writeCapturedBaseline(t, cfg, client, "access")
	client.set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print-missing.txt"))

	err := runCheck(t, cfg, testDeviceRole("access"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected missing route")
	}
	if !strings.Contains(err.Error(), "192.168.10.0/24") {
		t.Fatalf("got %v", err)
	}
}

func TestAccessMissingManagementAddressFails(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "access", "convergence_timeout: 1ms\n")
	client := accessClient(t).
		set(validate.CmdInterface, ros6Fixture(t, "interface-print.txt")).
		set(validate.CmdBridge, ros6Fixture(t, "bridge-print.txt")).
		set(validate.CmdBridgeVLAN, ros6Fixture(t, "bridge-vlan-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt")).
		set(validate.CmdIPAddress, ros6Fixture(t, "ip-address-print.txt"))
	writeCapturedBaseline(t, cfg, client, "access")
	client.set(validate.CmdIPAddress, ros6Fixture(t, "ip-address-print-missing.txt"))

	err := runCheck(t, cfg, testDeviceRole("access"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected missing management address")
	}
	if !strings.Contains(err.Error(), "192.0.2.10") {
		t.Fatalf("got %v", err)
	}
}

func TestAccessAdminUpPortDisabledFails(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "access", "convergence_timeout: 1ms\n")
	client := accessClient(t).
		set(validate.CmdInterface, ros6Fixture(t, "interface-print.txt")).
		set(validate.CmdBridge, ros6Fixture(t, "bridge-print.txt")).
		set(validate.CmdBridgeVLAN, ros6Fixture(t, "bridge-vlan-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt")).
		set(validate.CmdIPAddress, ros6Fixture(t, "ip-address-print.txt"))
	writeCapturedBaseline(t, cfg, client, "access")
	client.set(validate.CmdInterface, ros6Fixture(t, "interface-print-port-down.txt"))

	err := runCheck(t, cfg, testDeviceRole("access"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected admin-up port disabled")
	}
	if !strings.Contains(err.Error(), "ether2") {
		t.Fatalf("got %v", err)
	}
}

func accessClient(t *testing.T) *fakeClient {
	t.Helper()
	return newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt"))
}
