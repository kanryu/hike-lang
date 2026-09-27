package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// UseNativeBinaries reports whether test commands must come from bin/.
func UseNativeBinaries() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("HIKE_E2E_USE_NATIVE_BIN")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

// NativeBinDir returns the configured native command directory.
func NativeBinDir(root string) string {
	if value := os.Getenv("HIKE_E2E_BIN_DIR"); value != "" {
		return value
	}
	return filepath.Join(root, "bin")
}

func NativeExecutableName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

// SelectBinary selects a prebuilt bin command in native mode, or fallback in
// the normal mode. Native mode fails early when the requested artifact is
// missing instead of silently rebuilding a different compiler.
func SelectBinary(root, name, fallback string) (string, error) {
	if !UseNativeBinaries() {
		return fallback, nil
	}
	path := filepath.Join(NativeBinDir(root), NativeExecutableName(name))
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("native test binary %s is unavailable: %w", path, err)
	}
	return path, nil
}
