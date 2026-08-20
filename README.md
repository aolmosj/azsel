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

Azure CLI extensions are shared across all profiles via a common `~/.azsel/extensions/` directory, so you only need to install each extension once. Each tenant's `cliextensions` is a symlink to it, which means az finds the shared extensions through the filesystem no matter how the tenant is reached — you do not have to set `AZURE_EXTENSION_DIR` yourself.

Those directories are created `0700`, and `config.json` is written `0600`. Azure CLI protects its token cache itself, but leaves `azureProfile.json`, `az.sess` and its HTTP cache world-readable, so the directory bit is the only lever `azsel` has over who else on the machine can read them. Directories that already exist keep the permissions they have.

The location of `~/.azsel/` itself can be overridden with the `AZSEL_HOME` environment variable — useful for dotfile setups, containers, or keeping separate sets of profiles:

```bash
export AZSEL_HOME=~/work/azsel
```

There is a symmetry here: `azsel` exists because `az` honours `AZURE_CONFIG_DIR`, so `azsel` honours `AZSEL_HOME` in the same spirit.

Since a child process cannot modify the parent shell's environment, `azsel` writes export commands to a temporary file and a shell wrapper function sources it to set the variables in your current session. All visible output (TUI, messages) goes to **stderr**, keeping stdout clean.

That file is **per shell**: the wrapper points `AZSEL_SWITCH_FILE` at `~/.azsel/.switch.$$`, keyed by the shell's PID. A single shared file meant one terminal could consume and delete a switch another terminal had not sourced yet. Files left behind by a shell that died mid-switch are swept after 24 hours.

## Compatibility

### Shells

Shell integration is what lets `azsel use` change the tenant in your *current* session. "Supported" means `azsel init` finds your profile and the generated wrapper works there.

| Shell | Status | Notes |
|---|---|---|
| zsh | Supported | detected, installed into `~/.zshrc` |
| bash | Supported | `~/.bashrc`, falling back to `~/.bash_profile` on macOS. Tested down to bash 3.2, the version macOS ships |
| fish | Not supported | the wrapper uses `local`, `[[ ]]` and `source`; fish cannot parse it |
| sh / dash | Not supported | `[[ ]]` is not POSIX |

On an unsupported shell everything else still works. Run `azsel use <name>` and source the file it writes yourself — see [Use in scripts](#use-in-scripts). `azsel init` will tell you this rather than suggesting a line that would not work.

### Operating systems

| OS | Status |
|---|---|
| macOS | Supported — arm64 and amd64 |
| Linux | Supported — arm64 and amd64 |
| Windows | Not built, and the shell integration does not apply |

CI runs the test suite on Linux and macOS, exercising the wrapper under both bash and zsh on each.

### Dependencies

| Dependency | Requirement |
|---|---|
| Azure CLI | Any version honouring `AZURE_CONFIG_DIR`. Verified against 2.87.0; the practical floor is older but has not been pinned down |
| Go | Only needed to build from source. See `go.mod` for the version in force |

## Installation

### Prerequisites

- [Go](https://go.dev/dl/) — version as declared in `go.mod` (currently 1.24), only to build from source
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

Run the init command to automatically configure your shell:

```bash
azsel init
```

This detects your shell (`zsh` or `bash`), appends the integration line to your profile, and tells you to reload. Alternatively, add this line manually to your `~/.bashrc` or `~/.zshrc`:

```bash
eval "$(azsel init --print)"
```

Then reload your shell:

```bash
source ~/.zshrc  # or ~/.bashrc
```

This installs a shell wrapper that makes `azsel` and `azsel use <name>` automatically set `AZURE_CONFIG_DIR` in your current shell.

## Usage

### Add a tenant

```bash
$ azsel add
Tenant name (lowercase, alphanumeric, hyphens): contoso
Azure Tenant ID (GUID or domain): xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx

Logging in to tenant "contoso" (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)...
# Browser opens for Azure login...

Tenant "contoso" added successfully.
To activate: azsel use contoso
```

If shell integration is not set up, `azsel add` says so rather than leaving you to find out when `azsel use` appears to do nothing.

The tenant can be given as a GUID or as one of the tenant's verified domains, such as `contoso.onmicrosoft.com` — `az login --tenant` accepts both. Anything else is rejected before the browser opens.

Use `--device-code` to authenticate via device code flow instead of opening a browser (useful for remote/headless machines):

```bash
$ azsel add --device-code
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

### Set a default tenant

`azsel use` only changes the tenant in your current shell. To make a tenant the default for **every** new shell — and for anything else that runs `az`, including cron jobs and IDEs — point `~/.azure` at it:

```bash
$ azsel default contoso
Default tenant set to "contoso".
New shells will start on this tenant. Open one to try it,
or run 'azsel use contoso' to switch this shell now.
```

This works by making `~/.azure` a symlink to the tenant's profile, so no shell integration is needed for it — a fresh terminal, a script, a scheduled job all pick it up. `azsel use` in a shell still wins over the default for that shell, and a subshell inherits its parent's tenant rather than reverting.

If you already had a real `~/.azure` (an existing `az` session), it is **moved** to a backup under `~/.azsel/backups/`, never deleted:

```bash
$ azsel default contoso
Moved your existing ~/.azure to /Users/you/.azsel/backups/azure-20260220-153000
Default tenant set to "contoso".
```

Show the current default with `azsel default`, and remove it with `azsel default --clear`, which returns `az` to its own `~/.azure` and tells you where the backup is.

> Changing the default takes effect the next time `az` runs in any shell that has not run `azsel use` — `~/.azure` is resolved fresh each time. Shells where you ran `azsel use` keep their tenant.

### Switch tenant (interactive TUI)

Run `azsel` with no arguments to launch the interactive selector:

```bash
$ azsel
```

```
  Azure Tenants

   D contoso
     xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx

  *  fabrikam
     yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy

  enter: activate  d: set default  /: filter  q: quit
```

`*` marks the active tenant (this shell), `D` the default tenant (new shells). They can be different tenants, as above.

- Use arrow keys (`↑`/`↓`) or `j`/`k` to navigate
- Press `/` to fuzzy-search by tenant name or ID
- Press `Enter` to activate the selected tenant
- Press `d` to make the selected tenant the default (asks first, since it repoints `~/.azure`)
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

In scripts, point `AZSEL_SWITCH_FILE` at a file of your own and source it after running `azsel use`:

```bash
#!/bin/bash
export AZSEL_SWITCH_FILE="$(mktemp)"
command azsel use staging-tenant
source "$AZSEL_SWITCH_FILE"
rm -f "$AZSEL_SWITCH_FILE"

az webapp list --output table
```

With `AZSEL_SWITCH_FILE` unset, `azsel` falls back to `~/.azsel/.switch`, so scripts written against the older layout keep working.

## Testing

### Automated tests

```bash
go test ./...
```

The suite needs neither Azure nor network access. Tests that touch configuration point `AZSEL_HOME` at a temporary directory, so running them never reads or overwrites your real `~/.azsel/`.

```bash
go test -race -cover ./...   # what CI runs
```

A few tests exercise the real Azure CLI to pin how `az` resolves the default `~/.azure` symlink. They are opt-in so the default suite stays hermetic (no `az`, no network); run them with:

```bash
AZSEL_INTEGRATION=1 go test ./internal/config/
```

### Build and verify

```bash
go build -o azsel .
go vet ./...
gofmt -l .                   # must print nothing
golangci-lint run ./...      # config in .golangci.yml
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

# 4. Activate it (with shell integration)
azsel use <name>
echo $AZURE_CONFIG_DIR
# → ~/.azsel/tenants/<name>

# 5. Verify az CLI uses the right tenant
az account show

# 6. Launch the TUI
azsel
# Navigate, select a tenant, verify activation

# 7. Clean up
./azsel remove <name> -f
./azsel list
# → "No tenants configured."
```

## Debugging

If tenant switching isn't working as expected, enable debug mode:

```bash
export AZSEL_DEBUG=1
azsel use <name>
```

This outputs trace information to stderr showing:
- Which binary is being executed
- Whether the `.switch` file is created and sourced
- The resulting `AZURE_CONFIG_DIR` value

```
[azsel-debug] args: use contoso
[azsel-debug] switch file: /Users/you/.azsel/.switch.48231
[azsel-debug-go] binary: /usr/local/bin/azsel
[azsel-debug-go] writing /Users/you/.azsel/.switch.48231
Switched to tenant "contoso"
[azsel-debug] sourcing /Users/you/.azsel/.switch.48231
export AZURE_CONFIG_DIR=/Users/you/.azsel/tenants/contoso
[azsel-debug] AZURE_CONFIG_DIR=/Users/you/.azsel/tenants/contoso
```

Lines prefixed `[azsel-debug]` come from the shell wrapper, `[azsel-debug-go]` from the binary itself.

To disable, unset the variable:

```bash
unset AZSEL_DEBUG
```

## Commands reference

| Command | Description |
|---|---|
| `azsel` | Launch interactive TUI to select a tenant |
| `azsel init` | Set up shell integration (auto-detects shell profile) |
| `azsel init --print` | Print the shell function without modifying files |
| `azsel add` | Add a new tenant (interactive prompts + `az login`) |
| `azsel add --device-code` | Add a tenant using device code flow (no browser) |
| `azsel list` | List all configured tenants |
| `azsel use <name>` | Activate a tenant by name |
| `azsel default <name>` | Make a tenant the default for new shells |
| `azsel default` | Show the current default tenant |
| `azsel default --clear` | Remove the default |
| `azsel remove <name>` | Remove a tenant and its config directory |
| `azsel remove <name> -f` | Remove without confirmation |
| `azsel --version` | Show the current version |
| `azsel completion <shell>` | Generate shell completions (bash/zsh/fish/powershell) |

## Versioning

This project follows [Semantic Versioning](https://semver.org/). The version is injected at build time via `-ldflags`.

### Check the current version

```bash
$ azsel --version
azsel version 0.4.0
```

### Build with a specific version

```bash
go build -ldflags "-X main.version=0.4.0" -o azsel .
```

Without `-ldflags`, the version defaults to `dev`.

### Creating a release

Releases are automated via [GoReleaser](https://goreleaser.com/) and GitHub Actions. To create a new release:

```bash
# 1. Tag the commit with a semver tag
git tag v0.5.0

# 2. Push the tag to trigger the release workflow
git push origin v0.5.0
```

This will:
- Build binaries for linux/darwin (amd64/arm64)
- Create a GitHub Release with the binaries and checksums
- Generate a changelog from commit messages

### Download a release

Pre-built binaries are available on the [Releases](https://github.com/aolmosj/azsel/releases) page.

```bash
# Example: macOS arm64
VERSION=0.4.0
BASE=https://github.com/aolmosj/azsel/releases/download/v$VERSION

curl -LO $BASE/azsel_${VERSION}_darwin_arm64.tar.gz
curl -LO $BASE/checksums.txt
shasum -a 256 --ignore-missing -c checksums.txt

tar xzf azsel_${VERSION}_darwin_arm64.tar.gz
mv azsel /usr/local/bin/
```

Every release ships a `checksums.txt`; `--ignore-missing` checks only the archive you downloaded. On Linux, use `sha256sum` in place of `shasum -a 256`.

## License

MIT
