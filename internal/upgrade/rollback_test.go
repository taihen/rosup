package upgrade_test

import (
	"context"
	"strings"
	"testing"

	"github.com/taihen/rosup/internal/release"
	"github.com/taihen/rosup/internal/upgrade"
)

func TestRollbackRequiresToVersion(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	err := upgrade.Rollback(context.Background(), cfg, "router-01", "", world.opts())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--to-version") {
		t.Fatalf("got %v", err)
	}
	if world.dialCount() != 0 {
		t.Fatalf("dialed %d times", world.dialCount())
	}
}

func TestRollbackUnknownDevice(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	err := upgrade.Rollback(context.Background(), cfg, "missing", current, world.opts())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("got %v", err)
	}
	if world.dialCount() != 0 {
		t.Fatalf("dialed %d times", world.dialCount())
	}
}

func TestRollbackRequiresCompleteLocalRelease(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	sim := world.sim("router-01")
	sim.version = target

	err := upgrade.Rollback(context.Background(), cfg, "router-01", current, world.opts())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), current) && !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("got %v", err)
	}
	if len(sim.uploads) != 0 {
		t.Fatalf("uploaded %v", sim.uploads)
	}
}

func TestRollbackStagesCompleteLocalReleaseAndReboots(t *testing.T) {
	cfg, world := setup(t, device("router-01", "core-a", 10, nil))
	writeRelease(t, cfg, current, []release.File{
		npkVer("routeros", "arm", current),
		npkVer("wireless", "arm", current),
	})
	sim := world.sim("router-01")
	sim.version = target

	if err := upgrade.Rollback(context.Background(), cfg, "router-01", current, world.opts()); err != nil {
		t.Fatal(err)
	}
	got := sim.uploadedRemotes()
	if strings.Join(got, ",") != "routeros-arm-6.49.18.npk,wireless-6.49.18-arm.npk" {
		t.Fatalf("uploads %v", got)
	}
	if sim.reboots != 1 {
		t.Fatalf("reboots %d", sim.reboots)
	}
	if sim.version != current {
		t.Fatalf("version %q, want %q", sim.version, current)
	}
	if containsPrefix(sim.runs, "/system backup load") {
		t.Fatal("rollback must not load a binary backup")
	}
}
