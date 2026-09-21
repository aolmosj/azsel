package cmd

import (
	"fmt"

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
