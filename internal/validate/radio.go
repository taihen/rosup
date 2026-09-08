package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/transport"
)

type radioFacts struct {
	Interfaces []wirelessIface `json:"interfaces"`
	Peers      []string        `json:"peers"`
}

type wirelessIface struct {
	Name      string `json:"name"`
	SSID      string `json:"ssid"`
	Frequency string `json:"frequency"`
	Channel   string `json:"channel"`
	Disabled  bool   `json:"disabled"`
	Running   bool   `json:"running"`
}

func captureRadio(ctx context.Context, client transport.Client) (json.RawMessage, error) {
	facts, err := readRadio(ctx, client)
	if err != nil {
		return nil, err
	}
	return marshalFacts(facts)
}

func checkRadio(ctx context.Context, client transport.Client, baseline Baseline, _ discover.Facts) error {
	want, err := decodeRadio(baseline.RoleFacts)
	if err != nil {
		return err
	}
	have, err := readRadio(ctx, client)
	if err != nil {
		return fmt.Errorf("validate: %s: %w", baseline.Device, err)
	}
	haveIf := map[string]wirelessIface{}
	for _, w := range have.Interfaces {
		haveIf[w.Name] = w
	}
	for _, w := range want.Interfaces {
		got, ok := haveIf[w.Name]
		if !ok {
			return fmt.Errorf("validate: %s: radio: missing interface %s", baseline.Device, w.Name)
		}
		if !w.Disabled && got.Disabled {
			return fmt.Errorf("validate: %s: radio: interface %s is down", baseline.Device, w.Name)
		}
		if w.Running && !got.Running {
			return fmt.Errorf("validate: %s: radio: interface %s is down", baseline.Device, w.Name)
		}
		if w.SSID != "" && got.SSID != w.SSID {
			return fmt.Errorf("validate: %s: radio: interface %s ssid %s, want %s", baseline.Device, w.Name, got.SSID, w.SSID)
		}
		if w.Frequency != "" && got.Frequency != w.Frequency {
			return fmt.Errorf("validate: %s: radio: interface %s frequency %s, want %s", baseline.Device, w.Name, got.Frequency, w.Frequency)
		}
		if w.Channel != "" && got.Channel != w.Channel {
			return fmt.Errorf("validate: %s: radio: interface %s channel %s, want %s", baseline.Device, w.Name, got.Channel, w.Channel)
		}
	}
	havePeers := map[string]struct{}{}
	for _, p := range have.Peers {
		havePeers[strings.ToUpper(p)] = struct{}{}
	}
	for _, p := range want.Peers {
		if _, ok := havePeers[strings.ToUpper(p)]; !ok {
			return fmt.Errorf("validate: %s: radio: missing peer %s", baseline.Device, p)
		}
	}
	return nil
}

func readRadio(ctx context.Context, client transport.Client) (radioFacts, error) {
	wOut, err := runPrint(ctx, client, CmdWireless)
	if err != nil {
		return radioFacts{}, err
	}
	rOut, err := runPrint(ctx, client, CmdWirelessReg)
	if err != nil {
		return radioFacts{}, err
	}
	facts := radioFacts{
		Interfaces: parseWireless(wOut),
		Peers:      parseMACs(rOut),
	}
	if facts.Interfaces == nil {
		facts.Interfaces = []wirelessIface{}
	}
	if facts.Peers == nil {
		facts.Peers = []string{}
	}
	return facts, nil
}

func parseWireless(output string) []wirelessIface {
	var out []wirelessIface
	for _, rec := range parseKeyedRecords(output) {
		name := rec.fields["name"]
		if name == "" {
			continue
		}
		ch := rec.fields["channel-width"]
		if ch == "" {
			ch = rec.fields["channel"]
		}
		out = append(out, wirelessIface{
			Name:      name,
			SSID:      rec.fields["ssid"],
			Frequency: rec.fields["frequency"],
			Channel:   ch,
			Disabled:  disabled(rec.flags, rec.fields),
			Running:   running(rec.flags),
		})
	}
	return out
}

func decodeRadio(raw json.RawMessage) (radioFacts, error) {
	var facts radioFacts
	if len(raw) == 0 || string(raw) == "null" {
		return radioFacts{}, nil
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		return radioFacts{}, fmt.Errorf("validate: parse radio role facts: %w", err)
	}
	return facts, nil
}
