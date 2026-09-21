package cmd

import (
	"fmt"
	"os"

	"github.com/aolmosj/azsel/internal/azure"
	"github.com/aolmosj/azsel/internal/config"
)

// resolveTenant picks the tenant a command acts on: the one named in args, else
// the configured default, else an error asking for one. Shared by the commands
// that operate on a single tenant without a required positional name.
func resolveTenant(cfg *config.Config, args []string) (*config.Tenant, error) {
	if len(args) == 1 {
		t := cfg.FindTenant(args[0])
		if t == nil {
			return nil, fmt.Errorf("tenant %q not found", args[0])
		}
		return t, nil
	}
	if info, err := config.ResolveDefault(cfg); err == nil && info.State == config.DefaultSet {
		if t := cfg.FindTenant(info.Tenant); t != nil {
			return t, nil
		}
	}
	return nil, fmt.Errorf("no tenant given and no default set; pass a name or run 'azsel default <name>'")
}

// requireSession fails with an actionable message when a tenant's login has
// lapsed, so a command needing a valid session says so plainly (and points at
// 'azsel login') rather than surfacing a raw az error later. Callers gate on
// azure.Available first.
func requireSession(t *config.Tenant) error {
	if valid, _ := azure.SessionState(t.ConfigDir); !valid {
		return fmt.Errorf("the session for tenant %q has expired; run 'azsel login %s'", t.Name, t.Name)
	}
	return nil
}

// warnIfExpired notes, without failing, that a tenant a command just switched to
// or made default has a lapsed session — the point of doing so is to use it, and
// az would fail until re-login. Best-effort: skipped when the Azure CLI is not
// installed, so 'use' and 'default' keep working without az.
func warnIfExpired(t *config.Tenant) {
	if azure.Available() != nil {
		return
	}
	if valid, _ := azure.SessionState(t.ConfigDir); !valid {
		fmt.Fprintf(os.Stderr, "Note: tenant %q's session has expired; run 'azsel login %s'.\n", t.Name, t.Name)
	}
}
