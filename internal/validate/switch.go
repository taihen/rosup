package validate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/transport"
)

type switchFacts struct {
	Bridges    []bridgeIface `json:"bridges"`
	VLANs      []bridgeVLAN  `json:"vlans"`
	Interfaces []ifaceRow    `json:"interfaces"`
}

type bridgeIface struct {
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
	Running  bool   `json:"running"`
}

type bridgeVLAN struct {
	Bridge   string   `json:"bridge"`
	VLANIDs  string   `json:"vlan_ids"`
	Tagged   []string `json:"tagged"`
	Untagged []string `json:"untagged"`
}

type ifaceRow struct {
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
}

func captureSwitch(ctx context.Context, client transport.Client) (json.RawMessage, error) {
	facts, err := readSwitch(ctx, client)
	if err != nil {
		return nil, err
	}
	return marshalFacts(facts)
}

func checkSwitch(ctx context.Context, client transport.Client, baseline Baseline, _ discover.Facts) error {
	want, err := decodeSwitch(baseline.RoleFacts)
	if err != nil {
		return err
	}
	have, err := readSwitch(ctx, client)
	if err != nil {
		return fmt.Errorf("validate: %s: %w", baseline.Device, err)
	}
	return compareL2(baseline.Device, want, have)
}

func readSwitch(ctx context.Context, client transport.Client) (switchFacts, error) {
	bOut, err := runPrint(ctx, client, CmdBridge)
	if err != nil {
		return switchFacts{}, err
	}
	vOut, err := runPrint(ctx, client, CmdBridgeVLAN)
	if err != nil {
		return switchFacts{}, err
	}
	iOut, err := runPrint(ctx, client, CmdInterface)
	if err != nil {
		return switchFacts{}, err
	}
	return switchFactsFrom(bOut, vOut, iOut), nil
}

func switchFactsFrom(bridgeOut, vlanOut, ifaceOut string) switchFacts {
	facts := switchFacts{
		Bridges:    parseBridges(bridgeOut),
		VLANs:      parseBridgeVLANs(vlanOut),
		Interfaces: parseIfaces(ifaceOut),
	}
	if facts.Bridges == nil {
		facts.Bridges = []bridgeIface{}
	}
	if facts.VLANs == nil {
		facts.VLANs = []bridgeVLAN{}
	}
	if facts.Interfaces == nil {
		facts.Interfaces = []ifaceRow{}
	}
	return facts
}

func parseBridges(output string) []bridgeIface {
	var out []bridgeIface
	for _, rec := range parseKeyedRecords(output) {
		name := rec.fields["name"]
		if name == "" {
			continue
		}
		out = append(out, bridgeIface{
			Name:     name,
			Disabled: disabled(rec.flags, rec.fields),
			Running:  running(rec.flags),
		})
	}
	return out
}

func parseBridgeVLANs(output string) []bridgeVLAN {
	var out []bridgeVLAN
	for _, rec := range parseKeyedRecords(output) {
		ids := rec.fields["vlan-ids"]
		if ids == "" {
			continue
		}
		out = append(out, bridgeVLAN{
			Bridge:   rec.fields["bridge"],
			VLANIDs:  ids,
			Tagged:   splitSet(rec.fields["tagged"]),
			Untagged: splitSet(rec.fields["untagged"]),
		})
	}
	return out
}

func parseIfaces(output string) []ifaceRow {
	var out []ifaceRow
	for _, rec := range parseNamedRows(output) {
		name := rec.fields["name"]
		if name == "" {
			continue
		}
		out = append(out, ifaceRow{
			Name:     name,
			Disabled: disabled(rec.flags, rec.fields),
		})
	}
	return out
}

func compareL2(device string, want, have switchFacts) error {
	haveB := map[string]bridgeIface{}
	for _, b := range have.Bridges {
		haveB[b.Name] = b
	}
	for _, b := range want.Bridges {
		got, ok := haveB[b.Name]
		if !ok {
			return fmt.Errorf("validate: %s: missing bridge %s", device, b.Name)
		}
		if !b.Disabled && (got.Disabled || (b.Running && !got.Running)) {
			return fmt.Errorf("validate: %s: bridge %s is down", device, b.Name)
		}
	}

	haveV := map[string]bridgeVLAN{}
	for _, v := range have.VLANs {
		haveV[vlanKey(v)] = v
	}
	for _, v := range want.VLANs {
		got, ok := haveV[vlanKey(v)]
		if !ok {
			return fmt.Errorf("validate: %s: missing vlan %s", device, v.VLANIDs)
		}
		if !sameSet(got.Tagged, v.Tagged) || !sameSet(got.Untagged, v.Untagged) {
			return fmt.Errorf("validate: %s: vlan %s membership changed", device, v.VLANIDs)
		}
	}

	haveI := map[string]ifaceRow{}
	for _, i := range have.Interfaces {
		haveI[i.Name] = i
	}
	for _, i := range want.Interfaces {
		if i.Disabled {
			continue
		}
		got, ok := haveI[i.Name]
		if !ok {
			return fmt.Errorf("validate: %s: missing interface %s", device, i.Name)
		}
		if got.Disabled {
			return fmt.Errorf("validate: %s: admin-up port %s is disabled", device, i.Name)
		}
	}
	return nil
}

func vlanKey(v bridgeVLAN) string {
	return v.Bridge + "/" + v.VLANIDs
}

func decodeSwitch(raw json.RawMessage) (switchFacts, error) {
	var facts switchFacts
	if len(raw) == 0 || string(raw) == "null" {
		return switchFacts{}, nil
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		return switchFacts{}, fmt.Errorf("validate: parse switch role facts: %w", err)
	}
	return facts, nil
}
