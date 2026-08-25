package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

const ts = "20260220-120000"

// assertLinkTo checks ~/.azure is a symlink pointing at want.
func assertLinkTo(t *testing.T, home, want string) {
	t.Helper()
	azure := filepath.Join(home, ".azure")
	fi, err := os.Lstat(azure)
	if err != nil {
		t.Fatalf("~/.azure does not exist: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("~/.azure is not a link")
	}
	got, _ := os.Readlink(azure)
	if got != want {
		t.Errorf("~/.azure -> %q, wanted %q", got, want)
	}
}

func TestSetDefaultFromNothing(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	res, err := config.SetDefault(cfg, "contoso", ts)
	if err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if res.BackupPath != "" {
		t.Errorf("BackupPath = %q, should not back up anything", res.BackupPath)
	}
	if res.Repointed {
		t.Error("Repointed = true on fresh creation")
	}
	assertLinkTo(t, home, cfg.FindTenant("contoso").ConfigDir)
}

// The case behind #29: ~/.azure is a real directory with content.
func TestSetDefaultBacksUpRealDirectory(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	azure := filepath.Join(home, ".azure")
	if err := os.MkdirAll(azure, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	marker := filepath.Join(azure, "msal_token_cache.json")
	if err := os.WriteFile(marker, []byte("valuable session"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	res, err := config.SetDefault(cfg, "contoso", ts)
	if err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if res.BackupPath == "" {
		t.Fatal("no backup reported despite a real ~/.azure")
	}
	// The session is preserved in the backup, not destroyed.
	data, err := os.ReadFile(filepath.Join(res.BackupPath, "msal_token_cache.json"))
	if err != nil {
		t.Fatalf("the content did not survive the backup: %v", err)
	}
	if string(data) != "valuable session" {
		t.Errorf("backup content = %q", data)
	}
	assertLinkTo(t, home, cfg.FindTenant("contoso").ConfigDir)
}

func TestSetDefaultRepointsOwnLink(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso", "fabrikam")
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("first SetDefault: %v", err)
	}
	res, err := config.SetDefault(cfg, "fabrikam", ts)
	if err != nil {
		t.Fatalf("second SetDefault: %v", err)
	}
	if !res.Repointed {
		t.Error("Repointed = false when moving an existing link")
	}
	if res.BackupPath != "" {
		t.Error("backed up when repointing our own link; should not have")
	}
	assertLinkTo(t, home, cfg.FindTenant("fabrikam").ConfigDir)
}

// A foreign link is not touched.
func TestSetDefaultRefusesForeignLink(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	outside := t.TempDir()
	azure := filepath.Join(home, ".azure")
	if err := os.Symlink(outside, azure); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := config.SetDefault(cfg, "contoso", ts)
	if err == nil {
		t.Fatal("SetDefault clobbered a foreign link")
	}
	// The foreign link is still intact.
	got, _ := os.Readlink(azure)
	if got != outside {
		t.Errorf("the foreign link changed to %q", got)
	}
}

func TestSetDefaultRejectsUnknownTenant(t *testing.T) {
	_, cfg := azureSandbox(t, "contoso")
	if _, err := config.SetDefault(cfg, "ghost", ts); err == nil {
		t.Fatal("SetDefault accepted a nonexistent tenant")
	}
}

// Replaces a broken link that was ours (pointed at a deleted tenant).
func TestSetDefaultReplacesOwnBrokenLink(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso", "fabrikam")
	azure := filepath.Join(home, ".azure")
	// Link to a tenant we then delete: it dangles but is ours.
	gone := cfg.FindTenant("fabrikam").ConfigDir
	if err := os.Symlink(gone, azure); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("SetDefault on our own broken link: %v", err)
	}
	assertLinkTo(t, home, cfg.FindTenant("contoso").ConfigDir)
}

func TestSetDefaultCreatesSharedExtensionsLink(t *testing.T) {
	_, cfg := azureSandbox(t, "contoso")
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	shared, _ := config.ExtensionsDir()
	link := filepath.Join(cfg.FindTenant("contoso").ConfigDir, "cliextensions")
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("cliextensions is not a link: %v", err)
	}
	if got != shared {
		t.Errorf("cliextensions -> %q, wanted the shared %q", got, shared)
	}
}

// A preexisting real cliextensions (leftovers) is moved aside, not deleted.
func TestSetDefaultMovesAsideStaleExtensions(t *testing.T) {
	_, cfg := azureSandbox(t, "contoso")
	stale := filepath.Join(cfg.FindTenant("contoso").ConfigDir, "cliextensions")
	if err := os.MkdirAll(stale, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stale, "old.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if _, err := os.Stat(stale + ".bak"); err != nil {
		t.Errorf("the leftovers were not moved aside to .bak: %v", err)
	}
}

func TestClearDefault(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	res, err := config.ClearDefault(cfg)
	if err != nil {
		t.Fatalf("ClearDefault: %v", err)
	}
	if !res.Cleared {
		t.Error("Cleared = false despite a default")
	}
	if _, err := os.Lstat(filepath.Join(home, ".azure")); !os.IsNotExist(err) {
		t.Errorf("~/.azure still exists after clear (err=%v)", err)
	}
}

func TestClearDefaultReportsLatestBackup(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	azure := filepath.Join(home, ".azure")
	if err := os.MkdirAll(azure, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := config.SetDefault(cfg, "contoso", ts); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	res, err := config.ClearDefault(cfg)
	if err != nil {
		t.Fatalf("ClearDefault: %v", err)
	}
	if res.LatestBackup == "" {
		t.Error("clear did not report the existing backup")
	}
}

func TestClearDefaultNoDefault(t *testing.T) {
	_, cfg := azureSandbox(t)
	res, err := config.ClearDefault(cfg)
	if err != nil {
		t.Fatalf("ClearDefault with no default: %v", err)
	}
	if res.Cleared {
		t.Error("Cleared = true with no default")
	}
}

// clear must not touch az's native ~/.azure.
func TestClearDefaultLeavesNativeDirectory(t *testing.T) {
	home, cfg := azureSandbox(t)
	azure := filepath.Join(home, ".azure")
	if err := os.MkdirAll(azure, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := config.ClearDefault(cfg); err != nil {
		t.Fatalf("ClearDefault: %v", err)
	}
	if _, err := os.Stat(azure); err != nil {
		t.Errorf("clear deleted the native ~/.azure: %v", err)
	}
}

// The link rename is atomic: there is never a moment without ~/.azure. What is
// checked here is that a previous link is replaced without passing through an
// observable nonexistent state — at least that the result is correct.
func TestReplaceIsAtomicResult(t *testing.T) {
	home, cfg := azureSandbox(t, "a", "b")
	if _, err := config.SetDefault(cfg, "a", ts); err != nil {
		t.Fatalf("set a: %v", err)
	}
	if _, err := config.SetDefault(cfg, "b", ts); err != nil {
		t.Fatalf("set b: %v", err)
	}
	// No temporary file must be left behind.
	azure := filepath.Join(home, ".azure")
	if _, err := os.Lstat(azure + ".azsel-tmp." + itoa(os.Getpid())); !os.IsNotExist(err) {
		t.Error("a temporary link was left behind")
	}
	assertLinkTo(t, home, cfg.FindTenant("b").ConfigDir)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestClearDefaultRefusesForeign(t *testing.T) {
	home, cfg := azureSandbox(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".azure")); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := config.ClearDefault(cfg); err == nil {
		t.Fatal("ClearDefault touched a foreign link")
	}
}

func TestClearDefaultRemovesOwnBrokenLink(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	gone := cfg.FindTenant("contoso").ConfigDir
	if err := os.Symlink(gone, filepath.Join(home, ".azure")); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("setup: %v", err)
	}
	res, err := config.ClearDefault(cfg)
	if err != nil {
		t.Fatalf("ClearDefault on our own broken link: %v", err)
	}
	if !res.Cleared {
		t.Error("a broken link that was ours was not cleared")
	}
	if _, err := os.Lstat(filepath.Join(home, ".azure")); !os.IsNotExist(err) {
		t.Error("the broken link is still there")
	}
}

// clear must NOT clear a foreign broken link.
func TestClearDefaultLeavesForeignBrokenLink(t *testing.T) {
	home, cfg := azureSandbox(t)
	outside := filepath.Join(t.TempDir(), "missing")
	if err := os.Symlink(outside, filepath.Join(home, ".azure")); err != nil {
		t.Fatalf("setup: %v", err)
	}
	res, err := config.ClearDefault(cfg)
	if err != nil {
		t.Fatalf("ClearDefault: %v", err)
	}
	if res.Cleared {
		t.Error("a foreign broken link was cleared")
	}
}

// The reported backup is the most recent when there are several.
func TestClearReportsNewestOfSeveralBackups(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	backups := filepath.Join(home, ".azsel", "backups")
	if err := os.MkdirAll(backups, 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, name := range []string{"azure-20260101-000000", "azure-20260115-000000"} {
		if err := os.MkdirAll(filepath.Join(backups, name), 0700); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	// real ~/.azure, so SetDefault generates a backup with a later stamp.
	if err := os.MkdirAll(filepath.Join(home, ".azure"), 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := config.SetDefault(cfg, "contoso", "20260220-120001"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	res, err := config.ClearDefault(cfg)
	if err != nil {
		t.Fatalf("ClearDefault: %v", err)
	}
	if filepath.Base(res.LatestBackup) != "azure-20260220-120001" {
		t.Errorf("LatestBackup = %q, wanted the most recent", filepath.Base(res.LatestBackup))
	}
}

// Review bug: if EnsureSharedExtensionsLink fails, ~/.azure must not have been
// moved to backup already. The extensions link comes before touching
// ~/.azure, so a failure there leaves ~/.azure intact.
func TestSetDefaultDoesNotBackUpIfExtensionsLinkFails(t *testing.T) {
	home, cfg := azureSandbox(t, "contoso")
	azure := filepath.Join(home, ".azure")
	if err := os.MkdirAll(azure, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(azure, "tok"), []byte("session"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Force EnsureSharedExtensionsLink to fail: a file where the shared
	// extensions directory should go makes its creation fail.
	extPath := filepath.Join(home, ".azsel", "extensions")
	_ = os.RemoveAll(extPath)
	if err := os.WriteFile(extPath, nil, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := config.SetDefault(cfg, "contoso", "20260220-120000"); err == nil {
		t.Fatal("SetDefault did not fail despite the extensions error")
	}
	// ~/.azure is still the real directory, not a link nor gone.
	fi, err := os.Lstat(azure)
	if err != nil {
		t.Fatalf("~/.azure disappeared despite the failure: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("~/.azure became a link despite the extensions failure")
	}
	if _, err := os.Stat(filepath.Join(azure, "tok")); err != nil {
		t.Error("the content of ~/.azure did not survive: the session was moved without a new link")
	}
	// And no backup must have been left.
	if entries, _ := os.ReadDir(filepath.Join(home, ".azsel", "backups")); len(entries) > 0 {
		t.Error("a backup was created despite the operation failing")
	}
}
