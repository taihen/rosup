package validate_test

import (
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/validate"
)

func TestRadioInterfacesPeersAndChannelMatchBaseline(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "radio", "convergence_timeout: 1ms\n")
	client := radioClient(t).
		set(validate.CmdWireless, ros6Fixture(t, "wireless-print.txt")).
		set(validate.CmdWirelessReg, ros6Fixture(t, "wireless-registration-table.txt"))
	writeCapturedBaseline(t, cfg, client, "radio")

	if err := runCheck(t, cfg, testDeviceRole("radio"), client, newFakeClock()); err != nil {
		t.Fatal(err)
	}
	assertROS6Commands(t, client.runs, validate.CmdWireless, validate.CmdWirelessReg)
	for _, cmd := range client.runs {
		if strings.Contains(cmd, "wifi") || strings.Contains(cmd, "/interface wifi") {
			t.Fatalf("ROS7 wifi used: %s", cmd)
		}
	}
}

func TestRadioInterfaceDownFails(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "radio", "convergence_timeout: 1ms\n")
	client := radioClient(t).
		set(validate.CmdWireless, ros6Fixture(t, "wireless-print.txt")).
		set(validate.CmdWirelessReg, ros6Fixture(t, "wireless-registration-table.txt"))
	writeCapturedBaseline(t, cfg, client, "radio")
	client.set(validate.CmdWireless, ros6Fixture(t, "wireless-print-down.txt"))

	err := runCheck(t, cfg, testDeviceRole("radio"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected interface down")
	}
	if !strings.Contains(err.Error(), "wlan1") {
		t.Fatalf("got %v", err)
	}
}

func TestRadioMissingPeerFails(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "radio", "convergence_timeout: 1ms\n")
	client := radioClient(t).
		set(validate.CmdWireless, ros6Fixture(t, "wireless-print.txt")).
		set(validate.CmdWirelessReg, ros6Fixture(t, "wireless-registration-table.txt"))
	writeCapturedBaseline(t, cfg, client, "radio")
	client.set(validate.CmdWirelessReg, ros6Fixture(t, "wireless-registration-table-empty.txt"))

	err := runCheck(t, cfg, testDeviceRole("radio"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected missing peer")
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "4C:5E:0C:11:22:33") &&
		!strings.Contains(strings.ToLower(err.Error()), "peer") &&
		!strings.Contains(strings.ToLower(err.Error()), "registration") {
		t.Fatalf("got %v", err)
	}
}

func TestRadioChannelFrequencySSIDMustMatchBaseline(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "radio", "convergence_timeout: 1ms\n")
	client := radioClient(t).
		set(validate.CmdWireless, ros6Fixture(t, "wireless-print.txt")).
		set(validate.CmdWirelessReg, ros6Fixture(t, "wireless-registration-table.txt"))
	writeCapturedBaseline(t, cfg, client, "radio")
	client.set(validate.CmdWireless, ros6Fixture(t, "wireless-print-retune.txt"))

	err := runCheck(t, cfg, testDeviceRole("radio"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected channel/ssid mismatch")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "ssid") && !strings.Contains(msg, "frequency") && !strings.Contains(msg, "channel") {
		t.Fatalf("got %v", err)
	}
}

func radioClient(t *testing.T) *fakeClient {
	t.Helper()
	return newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt"))
}
