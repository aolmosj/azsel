# azsel

Azure tenant selector — manage multiple Azure CLI profiles from a single machine.

If you work with multiple Azure tenants (as a consultant, MSP, or multi-org engineer), switching between them with `az cli` is painful. There is no built-in profile system. `azsel` fixes this by leveraging the `AZURE_CONFIG_DIR` environment variable to maintain fully isolated az CLI configurations per tenant, with an interactive TUI for quick switching.

## How it works

Azure CLI stores its configuration (auth tokens, default subscription, etc.) in `~/.azure/`. By setting the `AZURE_CONFIG_DIR` environment variable, you can redirect `az` to use a different directory.

`azsel` creates a separate config directory per tenant under `~/.azsel/tenants/<name>/` and manages switching between them:

```
~/.azsel/
├── config.json              # Tenant metadata
└── tenants/
    ├── client-a/            # Isolated az CLI config for client-a
    │   └── ...
    ├── client-b/            # Isolated az CLI config for client-b
    │   └── ...
    └── internal/
        └── ...
```

Since a child process cannot modify the parent shell's environment, `azsel` prints `export AZURE_CONFIG_DIR=<path>` to **stdout** and all other output (TUI, messages) goes to **stderr**. A shell wrapper function `eval`s the output to set the variable in your current session.

## Installation

### Prerequisites

- [Go 1.22+](https://go.dev/dl/)
- [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli) (`az`) installed and available in `PATH`

### Build from source

```bash
git clone https://github.com/aolmosj/azsel.git
cd azsel
go build -o azsel .
```

Move the binary somewhere in your `PATH`:

```bash
mv azsel /usr/local/bin/
```

Or with `go install`:

```bash
go install github.com/aolmosj/azsel@latest
```

### Shell integration (required)

Add this function to your `~/.bashrc` or `~/.zshrc`:

```bash
azsel() {
  local result
  result=$(command azsel "$@")
  if [[ $? -eq 0 && -n "$result" ]]; then
    eval "$result"
  fi
}
```

Reload your shell:

```bash
source ~/.zshrc  # or ~/.bashrc
```

This wrapper is what makes `azsel` and `azsel use <name>` automatically set `AZURE_CONFIG_DIR` in your current shell. Without it, the export command is just printed to the terminal.

## Usage

### Add a tenant

```bash
$ azsel add
Tenant name (lowercase, alphanumeric, hyphens): contoso
Azure Tenant ID: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx

Logging in to tenant "contoso" (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)...
# Browser opens for Azure login...

Tenant "contoso" added successfully.
To activate: eval $(azsel use contoso)
```

### List tenants

```bash
$ azsel list
ACTIVE  NAME       TENANT ID                             CONFIG DIR
*       contoso    xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx  /Users/you/.azsel/tenants/contoso
        fabrikam   yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy  /Users/you/.azsel/tenants/fabrikam
```

The `*` marks the currently active tenant (matching `AZURE_CONFIG_DIR`).

### Switch tenant (by name)

```bash
$ azsel use fabrikam
Switched to tenant "fabrikam"

$ az account show --query '{tenant:tenantId, name:name}' -o table
Tenant                                Name
------------------------------------  --------
yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy  fabrikam
```

### Switch tenant (interactive TUI)

Run `azsel` with no arguments to launch the interactive selector:

```bash
$ azsel
```

```
  Azure Tenants

  * contoso
    xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx

    fabrikam
    yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy

  enter: activate  /: filter  q: quit
```

- Use arrow keys (`↑`/`↓`) or `j`/`k` to navigate
- Press `/` to fuzzy-search by tenant name or ID
- Press `Enter` to activate the selected tenant
- Press `q` or `Ctrl+C` to quit without changing anything

### Remove a tenant

```bash
$ azsel remove fabrikam
Remove tenant "fabrikam" and delete /Users/you/.azsel/tenants/fabrikam? [y/N] y
Tenant "fabrikam" removed.
```

Use `--force` / `-f` to skip the confirmation:

```bash
$ azsel remove fabrikam -f
Tenant "fabrikam" removed.
```

## Examples

### Full workflow: onboard a new client

```bash
# Add the tenant
azsel add
# Name: acme-corp
# Tenant ID: 11111111-1111-1111-1111-111111111111
# Complete browser login...

# Activate it
azsel use acme-corp

# Verify
az account show
# Shows acme-corp tenant details

# Do your work...
az group list
az aks list
```

### Quick switch between tenants

```bash
# Check where you are
azsel list

# Switch via TUI
azsel

# Or switch directly
azsel use client-b
```

### Use in scripts

Since `azsel use` outputs a plain export command, you can use it in scripts without the shell wrapper:

```bash
#!/bin/bash
eval $(command azsel use staging-tenant)
az webapp list --output table
```

## Testing

### Build and verify

```bash
go build -o azsel .
go vet ./...
```

### Manual test flow

```bash
# 1. Confirm no tenants exist yet
./azsel list
# → "No tenants configured. Run 'azsel add' to add one."

# 2. Add a tenant
./azsel add
# Follow the prompts, complete az login

# 3. Verify it was saved
./azsel list
# → Shows your tenant in the table

# 4. Activate it
eval $(./azsel use <name>)
echo $AZURE_CONFIG_DIR
# → ~/.azsel/tenants/<name>

# 5. Verify az CLI uses the right tenant
az account show

# 6. Launch the TUI
eval $(./azsel)
# Navigate, select a tenant, verify activation

# 7. Clean up
./azsel remove <name> -f
./azsel list
# → "No tenants configured."
```

## Commands reference

| Command | Description |
|---|---|
| `azsel` | Launch interactive TUI to select a tenant |
| `azsel add` | Add a new tenant (interactive prompts + `az login`) |
| `azsel list` | List all configured tenants |
| `azsel use <name>` | Activate a tenant by name |
| `azsel remove <name>` | Remove a tenant and its config directory |
| `azsel completion <shell>` | Generate shell completions (bash/zsh/fish/powershell) |

## License

MIT
