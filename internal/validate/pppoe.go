package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/progress"
	"github.com/taihen/rosup/internal/transport"
)

type pppoeFacts struct {
	Servers    []pppoeServer  `json:"servers"`
	UseRadius  string         `json:"use_radius"`
	Accounting string         `json:"accounting"`
	Radius     []radiusServer `json:"radius"`
	Sessions   []string       `json:"sessions"`
}

type pppoeServer struct {
	ServiceName string `json:"service_name"`
	Interface   string `json:"interface"`
	Disabled    bool   `json:"disabled"`
}

type radiusServer struct {
	Service  string `json:"service"`
	Address  string `json:"address"`
	Disabled bool   `json:"disabled"`
}

func capturePPPoE(ctx context.Context, client transport.Client) (json.RawMessage, error) {
	facts, err := readPPPoE(ctx, client)
	if err != nil {
		return nil, err
	}
	return marshalFacts(facts)
}

func checkPPPoE(ctx context.Context, client transport.Client, baseline Baseline, _ discover.Facts) error {
	want, err := decodePPPoE(baseline.RoleFacts)
	if err != nil {
		return err
	}
	have, err := readPPPoE(ctx, client)
	if err != nil {
		return fmt.Errorf("validate: %s: %w", baseline.Device, err)
	}
	if err := checkPPPoEServer(baseline.Device, want, have); err != nil {
		return err
	}
	if err := checkPPPoEAAA(baseline.Device, want, have); err != nil {
		return err
	}
	if sessionsOK(want.Sessions, have.Sessions) {
		return nil
	}
	clock := roleClock(ctx)
	timeout := roleProfile(ctx).SessionRestoreTimeout
	if timeout > 0 {
		roleProgress(ctx).Update("waiting for PPPoE sessions (" + progress.FormatDuration(timeout) + ")")
		clock.Sleep(timeout)
		have, err = readPPPoE(ctx, client)
		if err != nil {
			return fmt.Errorf("validate: %s: %w", baseline.Device, err)
		}
		if sessionsOK(want.Sessions, have.Sessions) {
			return nil
		}
	}
	missing := missingSessions(want.Sessions, have.Sessions)
	return fmt.Errorf("validate: %s: pppoe: missing sessions %s", baseline.Device, strings.Join(missing, ", "))
}

func checkPPPoEServer(device string, want, have pppoeFacts) error {
	haveByName := map[string]pppoeServer{}
	for _, s := range have.Servers {
		haveByName[serverKey(s)] = s
	}
	for _, s := range want.Servers {
		got, ok := haveByName[serverKey(s)]
		if !ok {
			return fmt.Errorf("validate: %s: pppoe: missing server %s", device, serverKey(s))
		}
		if !s.Disabled && got.Disabled {
			return fmt.Errorf("validate: %s: pppoe: server %s is disabled", device, serverKey(s))
		}
	}
	return nil
}

func checkPPPoEAAA(device string, want, have pppoeFacts) error {
	if want.UseRadius != "" && have.UseRadius != want.UseRadius {
		return fmt.Errorf("validate: %s: pppoe: AAA use-radius %s, want %s", device, have.UseRadius, want.UseRadius)
	}
	if want.Accounting != "" && have.Accounting != want.Accounting {
		return fmt.Errorf("validate: %s: pppoe: AAA accounting %s, want %s", device, have.Accounting, want.Accounting)
	}
	haveR := map[string]radiusServer{}
	for _, r := range have.Radius {
		haveR[radiusKey(r)] = r
	}
	for _, r := range want.Radius {
		got, ok := haveR[radiusKey(r)]
		if !ok {
			return fmt.Errorf("validate: %s: pppoe: missing radius %s", device, radiusKey(r))
		}
		if !r.Disabled && got.Disabled {
			return fmt.Errorf("validate: %s: pppoe: radius %s is disabled", device, radiusKey(r))
		}
	}
	return nil
}

func readPPPoE(ctx context.Context, client transport.Client) (pppoeFacts, error) {
	serverOut, err := runPrint(ctx, client, CmdPPPoEServer)
	if err != nil {
		return pppoeFacts{}, err
	}
	aaaOut, err := runPrint(ctx, client, CmdPPPAAA)
	if err != nil {
		return pppoeFacts{}, err
	}
	radiusOut, err := runPrint(ctx, client, CmdRADIUS)
	if err != nil {
		return pppoeFacts{}, err
	}
	activeOut, err := runPrint(ctx, client, CmdPPPActive)
	if err != nil {
		return pppoeFacts{}, err
	}
	aaa := parseKVBlock(aaaOut)
	facts := pppoeFacts{
		Servers:    parsePPPoEServers(serverOut),
		UseRadius:  strings.ToLower(aaa["use-radius"]),
		Accounting: strings.ToLower(aaa["accounting"]),
		Radius:     parseRADIUS(radiusOut),
		Sessions:   parsePPPSessions(activeOut),
	}
	if facts.Servers == nil {
		facts.Servers = []pppoeServer{}
	}
	if facts.Radius == nil {
		facts.Radius = []radiusServer{}
	}
	if facts.Sessions == nil {
		facts.Sessions = []string{}
	}
	return facts, nil
}

func parsePPPoEServers(output string) []pppoeServer {
	var out []pppoeServer
	for _, rec := range parseKeyedRecords(output) {
		name := rec.fields["service-name"]
		if name == "" {
			name = rec.fields["name"]
		}
		out = append(out, pppoeServer{
			ServiceName: name,
			Interface:   rec.fields["interface"],
			Disabled:    disabled(rec.flags, rec.fields),
		})
	}
	return out
}

func parseRADIUS(output string) []radiusServer {
	var out []radiusServer
	for _, rec := range parseKeyedRecords(output) {
		addr := rec.fields["address"]
		if addr == "" {
			continue
		}
		out = append(out, radiusServer{
			Service:  rec.fields["service"],
			Address:  addr,
			Disabled: disabled(rec.flags, rec.fields),
		})
	}
	return out
}

func parsePPPSessions(output string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, rec := range parseNamedRows(output) {
		name := rec.fields["name"]
		if name == "" || cidrRE.MatchString(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func decodePPPoE(raw json.RawMessage) (pppoeFacts, error) {
	var facts pppoeFacts
	if len(raw) == 0 || string(raw) == "null" {
		return pppoeFacts{}, nil
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		return pppoeFacts{}, fmt.Errorf("validate: parse pppoe role facts: %w", err)
	}
	return facts, nil
}

func serverKey(s pppoeServer) string {
	if s.ServiceName != "" {
		return s.ServiceName
	}
	return s.Interface
}

func radiusKey(r radiusServer) string {
	return strings.TrimSpace(r.Service + " " + r.Address)
}

func sessionsOK(want, have []string) bool {
	return len(missingSessions(want, have)) == 0
}

func missingSessions(want, have []string) []string {
	haveSet := map[string]struct{}{}
	for _, s := range have {
		haveSet[s] = struct{}{}
	}
	var missing []string
	for _, s := range want {
		if _, ok := haveSet[s]; !ok {
			missing = append(missing, s)
		}
	}
	return missing
}
