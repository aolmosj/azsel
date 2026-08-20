package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AzureDir is where the Azure CLI keeps its state when AZURE_CONFIG_DIR is
// unset: ~/.azure. The default-tenant mechanism turns this path into a
// symlink, so azsel needs to name it. It hangs off $HOME, not AZSEL_HOME —
// az knows nothing about the latter — which tests must keep in mind: they
// control both.
func AzureDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".azure"), nil
}

// DefaultState classifies what ~/.azure currently is. The default tenant is
// not stored anywhere — the symlink is the only source of truth — so reading
// it means inspecting that path.
type DefaultState int

const (
	// DefaultNone: ~/.azure does not exist. az will create its own directory
	// on first use. No default is set.
	DefaultNone DefaultState = iota
	// DefaultNative: ~/.azure is a real directory — az's own profile,
	// untouched by azsel. No default is set.
	DefaultNative
	// DefaultSet: ~/.azure is a symlink into ~/.azsel/tenants/<name> for a
	// tenant azsel knows. That tenant is the default.
	DefaultSet
	// DefaultForeign: ~/.azure is a symlink, but not to a tenant azsel
	// manages. Someone else set it up; azsel must not touch it.
	DefaultForeign
	// DefaultBroken: ~/.azure is a symlink whose target is gone. az fails
	// with a stack trace until this is repaired, so it has to be surfaced.
	DefaultBroken
)

// DefaultInfo is the resolved state of the default tenant.
type DefaultInfo struct {
	State DefaultState
	// Tenant is the default tenant's name, set only when State is DefaultSet.
	Tenant string
	// Target is the symlink's destination, set for every symlink state
	// (DefaultSet, DefaultForeign, DefaultBroken) for diagnostics.
	Target string
}

// ResolveDefault inspects ~/.azure and reports which tenant, if any, is the
// default. cfg is needed to tell a known tenant from an unknown one.
func ResolveDefault(cfg *Config) (DefaultInfo, error) {
	azure, err := AzureDir()
	if err != nil {
		return DefaultInfo{}, err
	}

	// Lstat, not Stat: Stat would follow the link and a symlink to a real
	// directory would be indistinguishable from a real directory.
	fi, err := os.Lstat(azure)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultInfo{State: DefaultNone}, nil
		}
		return DefaultInfo{}, fmt.Errorf("inspecting %s: %w", azure, err)
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		return DefaultInfo{State: DefaultNative}, nil
	}

	target, err := os.Readlink(azure)
	if err != nil {
		return DefaultInfo{}, fmt.Errorf("reading link %s: %w", azure, err)
	}
	// Resolve to an absolute path so the comparisons below hold even when the
	// link was written relative.
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(azure), target)
	}
	info := DefaultInfo{Target: target}

	// A dangling link is dangerous, so it is checked before anything else:
	// az breaks outright on it.
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			info.State = DefaultBroken
			return info, nil
		}
		return DefaultInfo{}, fmt.Errorf("resolving link target %s: %w", target, err)
	}

	name, ok, err := tenantNameFromTarget(target)
	if err != nil {
		return DefaultInfo{}, err
	}
	if ok && cfg.FindTenant(name) != nil {
		info.State = DefaultSet
		info.Tenant = name
		return info, nil
	}
	info.State = DefaultForeign
	return info, nil
}

// tenantNameFromTarget returns the tenant name when target is a direct child
// of the tenants directory, i.e. ~/.azsel/tenants/<name>. A link pointing
// deeper, or outside, is not one of azsel's tenant directories.
func tenantNameFromTarget(target string) (name string, ok bool, err error) {
	tenants, err := TenantsDir()
	if err != nil {
		return "", false, err
	}
	rel, err := filepath.Rel(tenants, target)
	if err != nil {
		// Different volumes on Windows; never a child here.
		return "", false, nil
	}
	// A direct child has no separator and is not "." or "..".
	if rel == "." || rel == ".." || strings.ContainsRune(rel, filepath.Separator) || strings.HasPrefix(rel, "..") {
		return "", false, nil
	}
	return rel, true, nil
}
