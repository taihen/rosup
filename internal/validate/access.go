package validate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/transport"
)

type accessFacts struct {
	switchFacts
	Prefixes  []string `json:"prefixes"`
	Addresses []string `json:"addresses"`
}

func captureAccess(ctx context.Context, client transport.Client) (json.RawMessage, error) {
	facts, err := readAccess(ctx, client)
	if err != nil {
		return nil, err
	}
	return marshalFacts(facts)
}

func checkAccess(ctx context.Context, client transport.Client, baseline Baseline, _ discover.Facts) error {
	want, err := decodeAccess(baseline.RoleFacts)
	if err != nil {
		return err
	}
	have, err := readAccess(ctx, client)
	if err != nil {
		return fmt.Errorf("validate: %s: %w", baseline.Device, err)
	}
	if err := compareL2(baseline.Device, want.switchFacts, have.switchFacts); err != nil {
		return err
	}
	haveP := map[string]struct{}{}
	for _, p := range have.Prefixes {
		haveP[p] = struct{}{}
	}
	for _, p := range want.Prefixes {
		if _, ok := haveP[p]; !ok {
			return fmt.Errorf("validate: %s: missing route %s", baseline.Device, p)
		}
	}
	haveA := map[string]struct{}{}
	for _, a := range have.Addresses {
		haveA[a] = struct{}{}
	}
	for _, a := range want.Addresses {
		if _, ok := haveA[a]; !ok {
			return fmt.Errorf("validate: %s: missing management address %s", baseline.Device, a)
		}
	}
	return nil
}

func readAccess(ctx context.Context, client transport.Client) (accessFacts, error) {
	l2, err := readSwitch(ctx, client)
	if err != nil {
		return accessFacts{}, err
	}
	rt, err := runPrint(ctx, client, CmdIPRoute)
	if err != nil {
		return accessFacts{}, err
	}
	addr, err := runPrint(ctx, client, CmdIPAddress)
	if err != nil {
		return accessFacts{}, err
	}
	facts := accessFacts{
		switchFacts: l2,
		Prefixes:    parseRoutePrefixes(rt),
		Addresses:   parseAddresses(addr),
	}
	if facts.Prefixes == nil {
		facts.Prefixes = []string{}
	}
	if facts.Addresses == nil {
		facts.Addresses = []string{}
	}
	return facts, nil
}

func parseAddresses(output string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, rec := range parseNamedRows(output) {
		if disabled(rec.flags, rec.fields) {
			continue
		}
		addr := rec.fields["address"]
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	return out
}

func decodeAccess(raw json.RawMessage) (accessFacts, error) {
	var facts accessFacts
	if len(raw) == 0 || string(raw) == "null" {
		return accessFacts{}, nil
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		return accessFacts{}, fmt.Errorf("validate: parse access role facts: %w", err)
	}
	return facts, nil
}
