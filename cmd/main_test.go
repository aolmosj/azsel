package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps the cmd unit tests hermetic. Commands now probe the session
// (default, use, pim, …) by shelling out to az; without this a real az on the
// runner would be invoked and write into a test's temporary config dir, which
// then fails t.TempDir cleanup ("directory not empty"). A no-op az placed first
// on PATH prevents that: tests that need specific az behavior use fakeAzureCLI
// (it prepends its own), and tests that need az absent use withoutAzureCLI (it
// clears PATH). Skipped when AZSEL_INTEGRATION is set, where a real az is wanted.
func TestMain(m *testing.M) {
	if os.Getenv("AZSEL_INTEGRATION") == "" {
		if dir, err := os.MkdirTemp("", "azsel-noop-az"); err == nil {
			if err := os.WriteFile(filepath.Join(dir, "az"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err == nil {
				os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
		}
	}
	os.Exit(m.Run())
}
