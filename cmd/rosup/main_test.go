package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func execute(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRootCmd()
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestVersion(t *testing.T) {
	out, _, err := execute(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("rosup %s (%s) %s\n", version, commit, date)
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestVersionDoesNotRequireConfig(t *testing.T) {
	t.Setenv("ROSUP_CONFIG", "/no/such/rosup.yaml")
	_, _, err := execute(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = execute(t, "--config", "/no/such/rosup.yaml", "version")
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoArgsExitsNonZero(t *testing.T) {
	_, _, err := execute(t)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUnknownCommandExitsNonZero(t *testing.T) {
	_, _, err := execute(t, "definitely-not-a-command")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStubCommandsNotImplemented(t *testing.T) {
	commands := [][]string{
		{"discover"},
		{"release", "sync"},
		{"release", "list"},
		{"plan"},
		{"upgrade"},
		{"verify"},
		{"rollback"},
		{"backup", "restore"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, _, err := execute(t, args...)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "not implemented") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestReleaseRequiresSubcommand(t *testing.T) {
	_, _, err := execute(t, "release")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBackupRequiresSubcommand(t *testing.T) {
	_, _, err := execute(t, "backup")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDiscoverMissingConfigFlag(t *testing.T) {
	_, _, err := execute(t, "--config", "/no/such/rosup.yaml", "discover")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("should fail on missing config before the stub, got %v", err)
	}
}

func TestPersistentFlagsRegistered(t *testing.T) {
	cmd := newRootCmd()
	for _, name := range []string{"config", "release", "group", "to-version", "file", "resume"} {
		if cmd.PersistentFlags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
}
