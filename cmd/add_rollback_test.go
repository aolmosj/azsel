package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

const testTenantID = "11111111-1111-1111-1111-111111111111"

// sandbox isolates azsel's configuration for a test.
func addSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	return home
}

// The happy path, end-to-end with a fake az that exits 0.
func TestAddSucceeds(t *testing.T) {
	home := addSandbox(t)
	fakeAzureCLI(t, "exit 0")
	feedStdin(t, "acme\n"+testTenantID+"\n")
	quiet(t)

	if err := run(t, newAddCmd()); err != nil {
		t.Fatalf("add: %v", err)
	}

	if fi, err := os.Stat(filepath.Join(home, "tenants", "acme")); err != nil || !fi.IsDir() {
		t.Fatalf("tenant directory was not created: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.FindTenant("acme")
	if got == nil {
		t.Fatal("the tenant was not saved to config.json")
	}
	if got.TenantID != testTenantID {
		t.Errorf("TenantID = %q, wanted %q", got.TenantID, testTenantID)
	}
}

// The failure that motivates this issue: az login fails and the directory stays.
func TestAddRemovesDirectoryItCreatedWhenLoginFails(t *testing.T) {
	home := addSandbox(t)
	fakeAzureCLI(t, "exit 1")
	feedStdin(t, "acme\n"+testTenantID+"\n")
	quiet(t)

	if err := run(t, newAddCmd()); err == nil {
		t.Fatal("add returned nil with az login failing")
	}

	if _, err := os.Stat(filepath.Join(home, "tenants", "acme")); !os.IsNotExist(err) {
		t.Errorf("orphaned directory was left behind (err=%v)", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FindTenant("acme") != nil {
		t.Error("config.json was left with a tenant whose login did not complete")
	}
}

// The counterpoint, and what makes this fix dangerous if done wrong: a
// preexisting directory may contain valid credentials from an earlier
// attempt. A failed login must not destroy them.
func TestAddKeepsPreexistingDirectoryWhenLoginFails(t *testing.T) {
	home := addSandbox(t)

	tenantDir := filepath.Join(home, "tenants", "acme")
	if err := os.MkdirAll(tenantDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	token := filepath.Join(tenantDir, "msal_token_cache.json")
	if err := os.WriteFile(token, []byte(`{"credentials":"valuable"}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	fakeAzureCLI(t, "exit 1")
	feedStdin(t, "acme\n"+testTenantID+"\n")
	quiet(t)

	if err := run(t, newAddCmd()); err == nil {
		t.Fatal("add returned nil with az login failing")
	}

	if _, err := os.Stat(tenantDir); err != nil {
		t.Fatalf("a preexisting directory was deleted: %v", err)
	}
	data, err := os.ReadFile(token)
	if err != nil {
		t.Fatalf("preexisting credentials were deleted: %v", err)
	}
	if string(data) != `{"credentials":"valuable"}` {
		t.Errorf("the credentials changed: %s", data)
	}
}

// An invalid name should be rejected without creating anything and without
// getting as far as asking for the tenant ID.
func TestAddRejectsInvalidNameWithoutTouchingDisk(t *testing.T) {
	home := addSandbox(t)
	fakeAzureCLI(t, "exit 0")
	feedStdin(t, "Acme Corp\n"+testTenantID+"\n")
	output := quiet(t)

	if err := run(t, newAddCmd()); err == nil {
		t.Fatal("add accepted an invalid name")
	}
	if _, err := os.Stat(filepath.Join(home, "tenants")); !os.IsNotExist(err) {
		t.Error("the tenants directory was created despite the invalid name")
	}
	// The stdin offset is no good for checking this: bufio.Reader reads in
	// blocks, not lines. What does prove it didn't go ahead is that the
	// second prompt was never printed.
	if got := output(); strings.Contains(got, "Azure Tenant ID") {
		t.Errorf("the tenant ID was requested despite the invalid name:\n%s", got)
	}
}

// A badly pasted tenant ID should die here, not at Azure: azsel's error is
// clearer and doesn't cost a trip to the browser.
func TestAddRejectsInvalidTenantIDBeforeCallingAz(t *testing.T) {
	home := addSandbox(t)
	fakeAzureCLI(t, `touch "$AZSEL_HOME/az-called"; exit 0`)
	feedStdin(t, "acme\nnot-a-tenant\n")
	quiet(t)

	err := run(t, newAddCmd())
	if err == nil {
		t.Fatal("add accepted an invalid tenant ID")
	}
	if !strings.Contains(err.Error(), "invalid tenant ID") {
		t.Errorf("error = %q, wanted it to explain the format", err)
	}
	if _, err := os.Stat(filepath.Join(home, "az-called")); !os.IsNotExist(err) {
		t.Error("az was invoked despite the invalid tenant ID")
	}
	if _, err := os.Stat(filepath.Join(home, "tenants", "acme")); !os.IsNotExist(err) {
		t.Error("the tenant directory was created despite the invalid tenant ID")
	}
}

// Normalization has to reach config.json: otherwise the same tenant written
// with different casing would appear as two distinct entries.
func TestAddStoresTenantIDNormalized(t *testing.T) {
	addSandbox(t)
	fakeAzureCLI(t, "exit 0")
	feedStdin(t, "acme\nAABBCCDD-1122-3344-5566-778899AABBCC\n")
	quiet(t)

	if err := run(t, newAddCmd()); err != nil {
		t.Fatalf("add: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tenant := cfg.FindTenant("acme")
	if tenant == nil {
		t.Fatal("the tenant was not saved")
	}
	if want := "aabbccdd-1122-3344-5566-778899aabbcc"; tenant.TenantID != want {
		t.Errorf("TenantID = %q, wanted %q", tenant.TenantID, want)
	}
}

// The rollback can't cover only the login. Between creating the tenant
// directory and writing config.json there are more ways to exit, and they all
// leave the same orphan behind. This one reproduces a real case: a file
// occupying the path of the extensions directory, which EnsureExtensionsDir
// rejects since #7.
func TestAddRemovesDirectoryWhenExtensionsDirFails(t *testing.T) {
	home := addSandbox(t)
	if err := os.WriteFile(filepath.Join(home, "extensions"), nil, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fakeAzureCLI(t, "exit 0")
	feedStdin(t, "acme\n"+testTenantID+"\n")
	quiet(t)

	if err := run(t, newAddCmd()); err == nil {
		t.Fatal("add returned nil with the extensions directory blocked")
	}
	if _, err := os.Stat(filepath.Join(home, "tenants", "acme")); !os.IsNotExist(err) {
		t.Errorf("orphaned directory was left behind (err=%v)", err)
	}
}

// And it can't leave it behind if saving config.json fails either: a tenant
// with a directory but no entry is exactly the state that #9 eliminates.
//
// Reaching the Save requires care. Putting a directory at config.json's path
// won't do: Load fails when the command starts, before anything is created,
// and the test would pass without having exercised anything. What we do is
// leave tenants/ and extensions/ already created and turn ~/.azsel
// read-only, so that everything succeeds up to the final write.
func TestAddRemovesDirectoryWhenConfigCannotBeSaved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	home := addSandbox(t)
	for _, dir := range []string{"tenants", "extensions"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0755); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	if err := os.Chmod(home, 0555); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Restore write permission so that t.TempDir can clean up.
	t.Cleanup(func() { _ = os.Chmod(home, 0755) })

	fakeAzureCLI(t, "exit 0")
	feedStdin(t, "acme\n"+testTenantID+"\n")
	quiet(t)

	if err := run(t, newAddCmd()); err == nil {
		t.Fatal("add returned nil despite being unable to save the configuration")
	}
	if _, err := os.Stat(filepath.Join(home, "tenants", "acme")); !os.IsNotExist(err) {
		t.Errorf("orphaned directory was left behind (err=%v)", err)
	}
}
