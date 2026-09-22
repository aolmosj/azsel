package cmd

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/aolmosj/azsel/internal/azure"
	"github.com/aolmosj/azsel/internal/config"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var check bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List all configured tenants",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.Tenants) == 0 {
				fmt.Fprintln(os.Stderr, "No tenants configured. Run 'azsel add' to add one.")
				return nil
			}
			currentDir := os.Getenv("AZURE_CONFIG_DIR")

			// "active" and "default" are different things: active is the
			// tenant this shell points at right now, default is the one new
			// shells will start on. A tenant can be either, both, or neither.
			def, defErr := config.ResolveDefault(cfg)

			// --check adds a STATUS column reporting whether each tenant's login
			// session is still usable, one az token acquisition per tenant.
			var session map[string]bool
			if check {
				if err := azure.Available(); err != nil {
					return err
				}
				session = checkSessions(cfg.Tenants)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			if check {
				fmt.Fprintln(w, "ACTIVE\tDEFAULT\tNAME\tSTATUS\tTENANT ID\tCONFIG DIR")
			} else {
				fmt.Fprintln(w, "ACTIVE\tDEFAULT\tNAME\tTENANT ID\tCONFIG DIR")
			}
			anyExpired := false
			for _, t := range cfg.Tenants {
				active := ""
				if t.ConfigDir == currentDir {
					active = "*"
				}
				isDefault := ""
				if defErr == nil && def.State == config.DefaultSet && strings.EqualFold(def.Tenant, t.Name) {
					isDefault = "D"
				}
				if check {
					status := "expired"
					if session[t.Name] {
						status = "ok"
					} else {
						anyExpired = true
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", active, isDefault, t.Name, status, t.TenantID, t.ConfigDir)
				} else {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", active, isDefault, t.Name, t.TenantID, t.ConfigDir)
				}
			}
			if err := w.Flush(); err != nil {
				return err
			}

			// A broken or foreign ~/.azure never shows as a default above, so
			// say why here — list is where someone looks when az misbehaves.
			if defErr == nil {
				switch def.State {
				case config.DefaultBroken:
					fmt.Fprintf(os.Stderr, "\nWarning: ~/.azure is a broken symlink to %s; az will fail until it is fixed.\n", def.Target)
					fmt.Fprintln(os.Stderr, "Run 'azsel default --clear' or 'azsel default <name>' to repair it.")
				case config.DefaultForeign:
					fmt.Fprintf(os.Stderr, "\nNote: ~/.azure is a symlink to %s that azsel did not create.\n", def.Target)
				}
			}
			if check && anyExpired {
				fmt.Fprintln(os.Stderr, "\nSome sessions have expired. Run 'azsel login <name>' to refresh.")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&check, "check", false, "check each tenant's login session (adds a STATUS column)")
	return c
}

// checkSessions probes each tenant's login session concurrently, returning
// name -> valid. Bounded so a long tenant list does not open too many az
// processes at once.
func checkSessions(tenants []config.Tenant) map[string]bool {
	session := make(map[string]bool, len(tenants))
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, 6)
	)
	for _, t := range tenants {
		wg.Add(1)
		sem <- struct{}{}
		go func(t config.Tenant) {
			defer wg.Done()
			defer func() { <-sem }()
			valid, _ := azure.SessionState(t.ConfigDir, t.TenantID)
			mu.Lock()
			session[t.Name] = valid
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	return session
}
