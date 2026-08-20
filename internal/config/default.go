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

// BackupsDir is where azsel parks a ~/.azure it had to move aside. Under
// ~/.azsel so all of azsel's state lives in one place, computed without
// creating anything.
func BackupsDir() (string, error) { return inBase("backups") }

// SetResult reports what SetDefault did, so the caller can tell the user.
type SetResult struct {
	Tenant string
	// BackupPath is set when a real ~/.azure was moved aside; empty otherwise.
	BackupPath string
	// Repointed is true when an existing azsel link was moved, false when the
	// link was created fresh.
	Repointed bool
}

// SetDefault makes name the default tenant by pointing ~/.azure at its
// profile. The symlink is the state; nothing is written to config.json.
//
// What happens to an existing ~/.azure follows the policy decided in #29:
// a real directory is moved to a timestamped backup (never destroyed), an
// azsel link is repointed, and a link azsel did not create is left alone with
// an error rather than clobbered.
func SetDefault(cfg *Config, name string, timestamp string) (SetResult, error) {
	tenant := cfg.FindTenant(name)
	if tenant == nil {
		return SetResult{}, fmt.Errorf("tenant %q not found", name)
	}

	info, err := ResolveDefault(cfg)
	if err != nil {
		return SetResult{}, err
	}
	azure, err := AzureDir()
	if err != nil {
		return SetResult{}, err
	}

	result := SetResult{Tenant: name}
	switch info.State {
	case DefaultNone:
		// Nothing in the way.
	case DefaultSet:
		result.Repointed = true // our own link; rename replaces it
	case DefaultNative:
		backup, err := backupAzure(azure, timestamp)
		if err != nil {
			return SetResult{}, err
		}
		result.BackupPath = backup
	case DefaultBroken:
		// A dangling link is ours to replace only if it pointed into our
		// tenants directory. One dangling to somewhere else was not put there
		// by azsel.
		if under, err := targetUnderTenants(info.Target); err != nil {
			return SetResult{}, err
		} else if !under {
			return SetResult{}, foreignLinkError(azure, info.Target)
		}
		result.Repointed = true
	case DefaultForeign:
		return SetResult{}, foreignLinkError(azure, info.Target)
	}

	// Share extensions through the filesystem: the default is reached via the
	// link with no AZURE_EXTENSION_DIR, so az would otherwise resolve
	// extensions inside the tenant. See #26.
	if err := EnsureSharedExtensionsLink(tenant.ConfigDir); err != nil {
		return SetResult{}, err
	}

	if err := replaceWithSymlink(tenant.ConfigDir, azure); err != nil {
		return SetResult{}, err
	}
	return result, nil
}

// ClearResult reports what ClearDefault did.
type ClearResult struct {
	// Cleared is true when a default link was removed.
	Cleared bool
	// LatestBackup is the newest ~/.azure backup on disk, if any, so the
	// caller can tell the user where their old profile went. Not restored
	// automatically — with several backups azsel would have to guess which.
	LatestBackup string
}

// ClearDefault removes the default link, returning az to its own ~/.azure. A
// real directory or a foreign link is left untouched.
func ClearDefault(cfg *Config) (ClearResult, error) {
	info, err := ResolveDefault(cfg)
	if err != nil {
		return ClearResult{}, err
	}
	azure, err := AzureDir()
	if err != nil {
		return ClearResult{}, err
	}

	var result ClearResult
	switch info.State {
	case DefaultSet:
		if err := os.Remove(azure); err != nil {
			return ClearResult{}, fmt.Errorf("removing default link: %w", err)
		}
		result.Cleared = true
	case DefaultBroken:
		// Only clear a broken link that was ours.
		under, err := targetUnderTenants(info.Target)
		if err != nil {
			return ClearResult{}, err
		}
		if under {
			if err := os.Remove(azure); err != nil {
				return ClearResult{}, fmt.Errorf("removing default link: %w", err)
			}
			result.Cleared = true
		}
	case DefaultForeign:
		return ClearResult{}, foreignLinkError(azure, info.Target)
	case DefaultNone, DefaultNative:
		// Nothing azsel put there.
	}

	if latest, err := latestBackup(); err == nil {
		result.LatestBackup = latest
	}
	return result, nil
}

func foreignLinkError(azure, target string) error {
	return fmt.Errorf("%s is a symlink to %s that azsel did not create; "+
		"refusing to replace it — remove it yourself if you want azsel to manage the default", azure, target)
}

// backupAzure moves a real ~/.azure into ~/.azsel/backups/azure-<timestamp>
// and returns the backup path. A move, not a copy: on the same volume it is
// instant and preserves everything.
func backupAzure(azure, timestamp string) (string, error) {
	backups, err := BackupsDir()
	if err != nil {
		return "", err
	}
	if _, err := ensureDir(backups); err != nil {
		return "", fmt.Errorf("creating backups directory: %w", err)
	}
	dest := filepath.Join(backups, "azure-"+timestamp)
	if err := os.Rename(azure, dest); err != nil {
		return "", fmt.Errorf("backing up %s to %s: %w", azure, dest, err)
	}
	return dest, nil
}

// latestBackup returns the most recent azure-* backup, or an error if none.
func latestBackup() (string, error) {
	backups, err := BackupsDir()
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(backups)
	if err != nil {
		return "", err
	}
	var newest string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "azure-") {
			continue
		}
		// Names are azure-<timestamp>; lexical order matches chronological
		// when the caller uses a sortable timestamp.
		if e.Name() > newest {
			newest = e.Name()
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no backups")
	}
	return filepath.Join(backups, newest), nil
}

// EnsureSharedExtensionsLink makes the tenant's cliextensions a symlink to the
// shared extensions directory, so extensions resolve to the same place
// whether the tenant is reached through the default link or through
// AZURE_EXTENSION_DIR. Idempotent: a correct link already in place is left be.
func EnsureSharedExtensionsLink(tenantDir string) error {
	shared, err := EnsureExtensionsDir()
	if err != nil {
		return err
	}
	link := filepath.Join(tenantDir, "cliextensions")

	fi, err := os.Lstat(link)
	switch {
	case err == nil && fi.Mode()&os.ModeSymlink != 0:
		if target, _ := os.Readlink(link); target == shared {
			return nil // already correct
		}
		if err := os.Remove(link); err != nil {
			return fmt.Errorf("replacing extensions link: %w", err)
		}
	case err == nil:
		// A real directory of stale extensions (leftovers predating the
		// shared dir). Move it aside rather than delete, then link.
		if err := os.Rename(link, link+".bak"); err != nil {
			return fmt.Errorf("moving aside stale extensions: %w", err)
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("inspecting %s: %w", link, err)
	}

	if err := os.Symlink(shared, link); err != nil {
		return fmt.Errorf("linking extensions: %w", err)
	}
	return nil
}

// replaceWithSymlink points linkPath at target atomically: it creates the new
// link under a temporary name and renames it over linkPath, which on POSIX
// replaces an existing file or symlink in one step, leaving no window where
// linkPath is missing. It assumes linkPath is absent or a symlink — a real
// directory there must be moved away first (rename cannot replace a
// directory), which SetDefault does via backupAzure.
func replaceWithSymlink(target, linkPath string) error {
	tmp := fmt.Sprintf("%s.azsel-tmp.%d", linkPath, os.Getpid())
	// Clear any leftover from an interrupted run.
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("creating link: %w", err)
	}
	if err := os.Rename(tmp, linkPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("installing link at %s: %w", linkPath, err)
	}
	return nil
}

// targetUnderTenants reports whether a path lies inside the tenants directory.
func targetUnderTenants(target string) (bool, error) {
	tenants, err := TenantsDir()
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(tenants, target)
	if err != nil {
		return false, nil
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}
