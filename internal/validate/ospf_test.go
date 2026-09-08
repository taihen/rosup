package validate_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/validate"
)

func TestOSPFAllowsFullAndTwoWayNeighbors(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", ospfProfileYAML())
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt")).
		set(validate.CmdOSPFNeighbor, ros6Fixture(t, "ospf-neighbor-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt"))
	writeCapturedBaseline(t, cfg, client, "ospf")

	if err := runCheck(t, cfg, testDeviceRole("ospf"), client, newFakeClock()); err != nil {
		t.Fatal(err)
	}
	assertROS6Commands(t, client.runs, validate.CmdOSPFNeighbor, validate.CmdIPRoute)
}

func TestOSPFFullToTwoWayIsFailedAdjacency(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", ospfProfileYAML())
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt")).
		set(validate.CmdOSPFNeighbor, ros6Fixture(t, "ospf-neighbor-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt"))
	writeCapturedBaseline(t, cfg, client, "ospf")
	client.set(validate.CmdOSPFNeighbor, ros6Fixture(t, "ospf-neighbor-print-degraded.txt"))

	err := runCheck(t, cfg, testDeviceRole("ospf"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected failed adjacency")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "adjacency") && !strings.Contains(msg, "2-way") && !strings.Contains(msg, "2way") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "192.0.2.2") {
		t.Fatalf("want neighbor id in error, got %v", err)
	}
}

func TestOSPFMissingBaselinePrefixFails(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", ospfProfileYAML())
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt")).
		set(validate.CmdOSPFNeighbor, ros6Fixture(t, "ospf-neighbor-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt"))
	writeCapturedBaseline(t, cfg, client, "ospf")
	client.set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print-missing.txt"))

	err := runCheck(t, cfg, testDeviceRole("ospf"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected missing prefix")
	}
	if !strings.Contains(err.Error(), "192.168.10.0/24") {
		t.Fatalf("got %v", err)
	}
}

func TestOSPFRouteCountToleranceAllowsExtras(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", `convergence_timeout: 1ms
neighbor_state_allow:
  - Full
  - 2-Way
route_count_tolerance: 1
`)
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt")).
		set(validate.CmdOSPFNeighbor, ros6Fixture(t, "ospf-neighbor-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt"))
	writeCapturedBaseline(t, cfg, client, "ospf")
	client.set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print-extra.txt"))

	if err := runCheck(t, cfg, testDeviceRole("ospf"), client, newFakeClock()); err != nil {
		t.Fatal(err)
	}
}

func TestOSPFRouteCountToleranceRejectsTooManyExtras(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", ospfProfileYAML())
	client := newFakeClient(t, target, []string{"routeros", "wireless"}, ros6Fixture(t, "log-print-system.txt")).
		set(validate.CmdOSPFNeighbor, ros6Fixture(t, "ospf-neighbor-print.txt")).
		set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print.txt"))
	writeCapturedBaseline(t, cfg, client, "ospf")
	client.set(validate.CmdIPRoute, ros6Fixture(t, "ip-route-print-extra.txt"))

	err := runCheck(t, cfg, testDeviceRole("ospf"), client, newFakeClock())
	if err == nil {
		t.Fatal("expected extra routes beyond tolerance")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "route") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadProfileOSPFFields(t *testing.T) {
	cfg := testConfig(t)
	writeProfile(t, cfg, "ospf", ospfProfileYAML())
	p, err := validate.LoadProfile(cfg, "ospf")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.NeighborStateAllow) != 2 || p.NeighborStateAllow[0] != "Full" || p.NeighborStateAllow[1] != "2-Way" {
		t.Fatalf("allow %+v", p.NeighborStateAllow)
	}
	if p.RouteCountTolerance != 0 {
		t.Fatalf("tolerance %d", p.RouteCountTolerance)
	}
}

func writeCapturedBaseline(t *testing.T, cfg *config.Config, client *fakeClient, role string) {
	t.Helper()
	raw, err := validate.CaptureRoleFacts(context.Background(), client, role)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || string(raw) == "{}" {
		t.Fatalf("capture returned empty role facts: %s", raw)
	}
	writeBaselineRole(t, cfg, sampleFacts(current), raw)
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("role facts json: %s", raw)
	}
	if len(probe) == 0 {
		t.Fatalf("empty role facts object: %s", raw)
	}
}

func assertROS6Commands(t *testing.T, runs []string, want ...string) {
	t.Helper()
	for _, cmd := range runs {
		if strings.Contains(cmd, "wifi") {
			t.Fatalf("ROS7 wifi command used: %s", cmd)
		}
	}
	for _, cmd := range want {
		found := false
		for _, got := range runs {
			if got == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing command %q in %v", cmd, runs)
		}
	}
}
