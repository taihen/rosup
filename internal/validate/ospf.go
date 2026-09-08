package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/transport"
)

type ospfFacts struct {
	Neighbors []ospfNeighbor `json:"neighbors"`
	Prefixes  []string       `json:"prefixes"`
}

type ospfNeighbor struct {
	RouterID  string `json:"router_id"`
	Address   string `json:"address"`
	Interface string `json:"interface"`
	State     string `json:"state"`
}

func captureOSPF(ctx context.Context, client transport.Client) (json.RawMessage, error) {
	facts, err := readOSPF(ctx, client)
	if err != nil {
		return nil, err
	}
	return marshalFacts(facts)
}

func checkOSPF(ctx context.Context, client transport.Client, baseline Baseline, _ discover.Facts) error {
	want, err := decodeOSPF(baseline.RoleFacts)
	if err != nil {
		return err
	}
	have, err := readOSPF(ctx, client)
	if err != nil {
		return fmt.Errorf("validate: %s: %w", baseline.Device, err)
	}
	profile := roleProfile(ctx)
	allow := map[string]struct{}{}
	for _, s := range profile.NeighborStateAllow {
		allow[normalizeState(s)] = struct{}{}
	}
	if len(allow) == 0 {
		allow[normalizeState("Full")] = struct{}{}
		allow[normalizeState("2-Way")] = struct{}{}
	}

	haveByID := map[string]ospfNeighbor{}
	for _, n := range have.Neighbors {
		haveByID[neighborID(n)] = n
	}
	for _, n := range want.Neighbors {
		id := neighborID(n)
		got, ok := haveByID[id]
		if !ok {
			return fmt.Errorf("validate: %s: ospf: missing neighbor %s", baseline.Device, id)
		}
		st := normalizeState(got.State)
		if _, ok := allow[st]; !ok {
			return fmt.Errorf("validate: %s: ospf: neighbor %s state %s is not allowed", baseline.Device, id, got.State)
		}
		if normalizeState(n.State) == "full" && st == "2way" {
			return fmt.Errorf("validate: %s: ospf: neighbor %s: adjacency dropped from Full to 2-Way", baseline.Device, id)
		}
	}

	wantSet := map[string]struct{}{}
	for _, p := range want.Prefixes {
		wantSet[p] = struct{}{}
	}
	haveSet := map[string]struct{}{}
	for _, p := range have.Prefixes {
		haveSet[p] = struct{}{}
	}
	for _, p := range want.Prefixes {
		if _, ok := haveSet[p]; !ok {
			return fmt.Errorf("validate: %s: ospf: missing route %s", baseline.Device, p)
		}
	}
	var extra int
	for _, p := range have.Prefixes {
		if _, ok := wantSet[p]; !ok {
			extra++
		}
	}
	if extra > profile.RouteCountTolerance {
		return fmt.Errorf("validate: %s: ospf: %d extra routes, tolerance %d", baseline.Device, extra, profile.RouteCountTolerance)
	}
	return nil
}

func readOSPF(ctx context.Context, client transport.Client) (ospfFacts, error) {
	nb, err := runPrint(ctx, client, CmdOSPFNeighbor)
	if err != nil {
		return ospfFacts{}, err
	}
	rt, err := runPrint(ctx, client, CmdIPRoute)
	if err != nil {
		return ospfFacts{}, err
	}
	facts := ospfFacts{
		Neighbors: parseOSPFNeighbors(nb),
		Prefixes:  parseRoutePrefixes(rt),
	}
	if facts.Neighbors == nil {
		facts.Neighbors = []ospfNeighbor{}
	}
	if facts.Prefixes == nil {
		facts.Prefixes = []string{}
	}
	return facts, nil
}

func parseOSPFNeighbors(output string) []ospfNeighbor {
	var out []ospfNeighbor
	for _, rec := range parseKeyedRecords(output) {
		n := ospfNeighbor{
			RouterID:  rec.fields["router-id"],
			Address:   rec.fields["address"],
			Interface: rec.fields["interface"],
			State:     rec.fields["state"],
		}
		if neighborID(n) == "" {
			continue
		}
		out = append(out, n)
	}
	return out
}

func decodeOSPF(raw json.RawMessage) (ospfFacts, error) {
	var facts ospfFacts
	if len(raw) == 0 || string(raw) == "null" {
		return ospfFacts{}, nil
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		return ospfFacts{}, fmt.Errorf("validate: parse ospf role facts: %w", err)
	}
	if facts.Neighbors == nil {
		facts.Neighbors = []ospfNeighbor{}
	}
	if facts.Prefixes == nil {
		facts.Prefixes = []string{}
	}
	return facts, nil
}

func neighborID(n ospfNeighbor) string {
	if n.RouterID != "" {
		return n.RouterID
	}
	return strings.TrimSpace(n.Address + " " + n.Interface)
}

func normalizeState(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}
