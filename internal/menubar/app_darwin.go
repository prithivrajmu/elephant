//go:build darwin

package menubar

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// A real app bundle is required for macOS to place the status item on screen.
// A plain osascript process creates an accessibility item but no visible icon.
// Keep the compiler's bundle metadata: the system applet launcher depends on it.
// The script switches to accessory mode after launch instead of LSUIElement.

func appExecutable(ctx context.Context) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("menubar: find native app cache: %w", err)
	}
	base := filepath.Join(cache, "elephant", "menu-bar")
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", fmt.Errorf("menubar: create native app cache: %w", err)
	}
	sum := sha256.Sum256([]byte(script))
	destination := filepath.Join(base, fmt.Sprintf("%x", sum[:12]))
	executable := filepath.Join(destination, "Elephant.app", "Contents", "MacOS", "applet")
	if info, err := os.Stat(executable); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return "", fmt.Errorf("menubar: cached native app is not executable: %s", executable)
		}
		return executable, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("menubar: inspect native app cache: %w", err)
	}

	stage, err := os.MkdirTemp(base, ".build-")
	if err != nil {
		return "", fmt.Errorf("menubar: stage native app: %w", err)
	}
	defer os.RemoveAll(stage)
	source := filepath.Join(stage, "menu-bar.js")
	if err := os.WriteFile(source, []byte(script), 0600); err != nil {
		return "", fmt.Errorf("menubar: write native script: %w", err)
	}
	bundle := filepath.Join(stage, "Elephant.app")
	if err := nativeTool(ctx, "/usr/bin/osacompile", "-l", "JavaScript", "-o", bundle, source); err != nil {
		return "", err
	}
	if err := os.Remove(source); err != nil {
		return "", fmt.Errorf("menubar: remove staged native script: %w", err)
	}
	if err := os.Rename(stage, destination); err != nil {
		// Another private companion may have completed the same content-addressed build.
		if info, statErr := os.Stat(executable); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return executable, nil
		}
		return "", fmt.Errorf("menubar: publish native app: %w", err)
	}
	return executable, nil
}

func nativeTool(ctx context.Context, executable string, args ...string) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.WaitDelay = 3 * time.Second
	var output limitedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("menubar: %s: %w: %s", filepath.Base(executable), err, strings.TrimSpace(output.String()))
	}
	return nil
}
