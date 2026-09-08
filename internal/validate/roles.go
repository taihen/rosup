package validate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/taihen/rosup/internal/transport"
)

type CaptureFunc func(ctx context.Context, client transport.Client) (json.RawMessage, error)

var RoleCaptures = map[string]CaptureFunc{}

type roleCtxKey struct{}

type roleMeta struct {
	clock   Clock
	profile Profile
}

func init() {
	RoleChecks["ospf"] = checkOSPF
	RoleChecks["pppoe"] = checkPPPoE
	RoleChecks["radio"] = checkRadio
	RoleChecks["switch"] = checkSwitch
	RoleChecks["access"] = checkAccess
	RoleCaptures["ospf"] = captureOSPF
	RoleCaptures["pppoe"] = capturePPPoE
	RoleCaptures["radio"] = captureRadio
	RoleCaptures["switch"] = captureSwitch
	RoleCaptures["access"] = captureAccess
}

func CaptureRoleFacts(ctx context.Context, client transport.Client, role string) (json.RawMessage, error) {
	fn := RoleCaptures[role]
	if fn == nil {
		return json.RawMessage("{}"), nil
	}
	raw, err := fn(ctx, client)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	return raw, nil
}

func withRoleMeta(ctx context.Context, clock Clock, profile Profile) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if clock == nil {
		clock = realClock{}
	}
	return context.WithValue(ctx, roleCtxKey{}, roleMeta{clock: clock, profile: profile})
}

func roleClock(ctx context.Context) Clock {
	if m, ok := ctx.Value(roleCtxKey{}).(roleMeta); ok && m.clock != nil {
		return m.clock
	}
	return realClock{}
}

func roleProfile(ctx context.Context) Profile {
	if m, ok := ctx.Value(roleCtxKey{}).(roleMeta); ok {
		return m.profile
	}
	return Profile{}
}

func marshalFacts(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("validate: marshal role facts: %w", err)
	}
	return raw, nil
}

func runPrint(ctx context.Context, client transport.Client, cmd string) (string, error) {
	out, err := client.Run(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("%s: %w", cmd, err)
	}
	return out, nil
}
