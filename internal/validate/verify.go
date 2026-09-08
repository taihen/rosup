package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/transport"
)

func Verify(ctx context.Context, req Request) error {
	if req.Config == nil {
		return errors.New("validate: nil config")
	}
	if req.Device.Name == "" {
		return errors.New("validate: device is required")
	}
	if req.Dial == nil {
		req.Dial = transport.Dial
	}
	if req.Clock == nil {
		req.Clock = realClock{}
	}

	profileName := req.Profile
	if profileName == "" {
		profileName = req.Device.ValidationProfile
	}
	if profileName == "" {
		profileName = req.Device.Role
	}

	dir, err := lastBaselineDir(req.Config, req.Device.Name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		req.Target = filepath.Base(dir)
		return Check(ctx, req)
	}

	target, err := snapshotNow(ctx, req, profileName)
	if err != nil {
		return err
	}
	req.Target = target
	return Check(ctx, req)
}

func snapshotNow(ctx context.Context, req Request, profileName string) (string, error) {
	port := req.Device.Port
	if port == 0 {
		port = req.Config.SSH.DefaultPort
	}
	client, err := req.Dial(ctx, req.Config.SSH, req.Device.Address, port)
	if err != nil {
		return "", fmt.Errorf("validate: %s: %w: %w", req.Device.Name, ErrUnreachable, err)
	}
	defer func() { _ = client.Close() }()

	facts, err := factsFromClient(ctx, req.Device.Name, client)
	if err != nil {
		return "", err
	}
	roleFacts, err := CaptureRoleFacts(ctx, client, profileName)
	if err != nil {
		return "", fmt.Errorf("validate: %s: %w", req.Device.Name, err)
	}
	dir, err := JobDir(req.Config, req.Device.Name, facts.Version)
	if err != nil {
		return "", err
	}
	if err := WriteBaseline(dir, FromFacts(req.Device.Name, facts, true, roleFacts)); err != nil {
		return "", err
	}
	return facts.Version, nil
}

func lastBaselineDir(cfg *config.Config, device string) (string, error) {
	if cfg.DataDir == "" {
		return "", errors.New("validate: data_dir is required")
	}
	if err := safeName("device", device); err != nil {
		return "", err
	}
	base, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return "", fmt.Errorf("validate: resolve data_dir: %w", err)
	}
	root := filepath.Join(base, device)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", os.ErrNotExist
		}
		return "", fmt.Errorf("validate: read %s: %w", root, err)
	}

	var best string
	var bestTime time.Time
	found := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := safeName("release", e.Name()); err != nil {
			continue
		}
		path := filepath.Join(root, e.Name(), baselineFile)
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !found || st.ModTime().After(bestTime) {
			found = true
			bestTime = st.ModTime()
			best = filepath.Join(root, e.Name())
		}
	}
	if !found {
		return "", os.ErrNotExist
	}
	return best, nil
}
