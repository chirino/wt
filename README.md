# wt - Git Worktree Manager

A CLI tool that simplifies managing Git worktrees as sibling directories. It wraps Git's native `git worktree` command with devcontainer integration, making it easy to work on multiple branches simultaneously with full isolation.

## Why use wt?

Git worktrees let you check out multiple branches at the same time, each in its own directory. This is useful when you need to:

- **Work on multiple features or bug fixes in parallel** without stashing or committing incomplete work
- **Run long builds or tests on one branch** while continuing development on another
- **Review a pull request** without disrupting your current working directory
- **Compare behavior across branches** side by side

`wt` makes worktrees easy to manage and adds devcontainer support so each worktree can run in its own isolated container with its own network, ports, and services.

## Why a SOCKS proxy instead of port forwarding?

When running multiple worktrees simultaneously, each with its own devcontainer, port forwarding quickly becomes a problem. If every container runs a web server on port 8080, you can only forward one of them to `localhost:8080` at a time — the rest require manual remapping to different host ports, which breaks hardcoded URLs, OAuth redirect URIs, and service-to-service links.

`wt` solves this by running a SOCKS5 proxy inside each devcontainer and exposing it on a unique host port. Because SOCKS5 proxies the TCP connection itself rather than forwarding a single port, you can reach **any port on any service** inside the container through a single DYNAMIC proxy port — without remapping anything. The services inside the container keep their natural ports (8080, 5432, 6379, etc.) and your host-side tools connect through the proxy instead:

```bash
curl --proxy socks5h://127.0.0.1:$(wt proxy-port) http://127.0.0.1:8080
```

The `socks5h` scheme tells curl to resolve the hostname inside the container, so names like `localhost` or internal DNS names refer to the container's network, not the host's. This means:

- **No port conflicts** between worktrees — each proxy listens on a different host port, and container-side ports never need to change
- **All services reachable** — databases, APIs, and UIs on any port are accessible through one proxy entry point
- **URLs stay canonical** — no remapping means OAuth callbacks, API base URLs, and inter-service addresses work exactly as they do inside the container

VS Code's built-in browser and the `wt chrome` / `wt playwright` commands are pre-configured to route traffic through the worktree's proxy automatically.

## Why sibling directories?

`wt` places worktrees as siblings of the main repo rather than inside it (e.g., `myproject@feature` next to `myproject/`). This matters for several practical reasons:

- **Git works inside the devcontainer** - If the devcontainer mounts the parent directory of the repo (a common pattern to make all sibling worktrees visible inside the container), git commands work correctly in every worktree because each one is a proper git worktree under that shared parent mount
- **Clear visual grouping** - In your file manager or shell, all worktrees for a project appear together, named `project@branch`, making it obvious what each directory is

## Features

- **Sibling directory layout** - Worktrees are created as siblings of the main repo (e.g., `myproject@feature` next to `myproject/`)
- **Environment file copying** - Automatically copies all `.env*` files from the project root to new worktrees
- **Devcontainer support** - Start, build, and exec into per-worktree devcontainers
- **VS Code integration** - Open worktrees in VS Code with devcontainer attach and per-worktree profile isolation
- **SOCKS proxy per worktree** - Each devcontainer gets a dedicated proxy port for accessing container services from the host
- **Shell navigation** - Quickly open a shell in any worktree
- **Shell completion** - Tab completion for bash, zsh, fish, and PowerShell

## Installation

```bash
go install github.com/chirino/wt@latest
```

Or build from source:

```bash
git clone https://github.com/chirino/wt.git
cd wt
go build -o wt .
```

## Usage

### Create a worktree

```bash
wt add feature-xyz
```

Creates a worktree at `../myproject@feature-xyz` (sibling to your main repo) detached at the current HEAD. Automatically:
- Copies all `.env*` files from the root of the current project

### List worktrees

```bash
wt ls
```

### Navigate to a worktree

```bash
wt cd feature-xyz
```

Opens a new shell in the worktree directory. Without arguments, opens a shell in the main repo root.

### Open in VS Code

```bash
wt code feature-xyz
```

If the worktree has a `.devcontainer/devcontainer.json`, this will:
1. Run `devcontainer up` to start the container
2. Open VS Code attached to the running container
3. If the devcontainer has a SOCKS5 proxy running (port 1080):
   - Use a per-worktree VS Code profile (`.vscode-profile/`) to avoid settings conflicts
   - Route VS Code network traffic through the proxy

Without a devcontainer, it opens the directory in VS Code directly. Use `-c` to auto-create.

### Devcontainer commands

Scaffold a `.devcontainer/` with SOCKS5 proxy support:

```bash
wt init
```

Start a worktree's devcontainer:

```bash
wt up feature-xyz
```

Build a worktree's devcontainer:

```bash
wt build feature-xyz
```

Start a shell inside the devcontainer:

```bash
wt exec
```

Run a command inside the devcontainer:

```bash
wt exec -- make test
wt exec feature-xyz -- npm run dev
```

Use `.` to refer to the current worktree:

```bash
wt exec . -- go test ./...
```

Recreate the devcontainer from scratch (down + up):

```bash
wt bounce feature-xyz
```

### Access container services from the host

Each devcontainer gets a dedicated SOCKS5 proxy. Get the port with:

```bash
wt proxy-port feature-xyz
# or from within the worktree:
wt proxy-port
```

Then use it to reach services inside the container:

```bash
curl --proxy socks5h://127.0.0.1:$(wt proxy-port) http://127.0.0.1:8080
```

### Utility commands

```bash
wt name          # Print the current worktree name
wt dir           # Print the current worktree root directory
wt proxy-port    # Print the SOCKS proxy port for the current worktree
```

### Remove a worktree

```bash
wt rm feature-xyz
```

## Command reference

**Worktree commands**

| Command | Description |
|---|---|
| `wt add <name>` | Create a new worktree |
| `wt ls` | List all sibling worktrees |
| `wt rm <name> [git-args...]` | Remove a worktree and clean up its directory |
| `wt cd [name]` | Open a shell in the worktree directory |
| `wt code [name]` | Open the worktree in VS Code |
| `wt name` | Print the current worktree name |
| `wt dir` | Print the current worktree root directory |

**Devcontainer commands**

| Command | Description |
|---|---|
| `wt init` | Scaffold a `.devcontainer/` with SOCKS5 proxy support |
| `wt up [name] [devcontainer-args...]` | Start the worktree's devcontainer |
| `wt down [name]` | Stop and remove the worktree's devcontainer |
| `wt bounce [name]` | Recreate the worktree's devcontainer (down + up) |
| `wt build [name] [devcontainer-args...]` | Build the worktree's devcontainer image |
| `wt exec [name] -- <cmd> [args...]` | Run a command inside the worktree's devcontainer |

**SOCKS5 Proxy & Browser commands**

| Command | Description |
|---|---|
| `wt proxy-port [name]` | Print the host port of the worktree's SOCKS5 proxy |
| `wt chrome [name] [-- chrome-args...]` | Open Chrome with the worktree's proxy and an isolated profile |
| `wt playwright [name] [-- playwright-args...]` | Open a Playwright browser with the worktree's proxy |
| `wt curl [name] [-- curl-args...]` | Run curl through the worktree's SOCKS5 proxy |

**Setup commands**

| Command | Description |
|---|---|
| `wt skill` | Print the ai agent SKILL.md file for the wt command |
| `wt completion <shell>` | Generate shell completion scripts |

## Shell completion

```bash
# Bash
wt completion bash > /etc/bash_completion.d/wt

# Zsh
wt completion zsh > "${fpath[1]}/_wt"

# Fish
wt completion fish > ~/.config/fish/completions/wt.fish

# PowerShell
wt completion powershell > wt.ps1
```

## License

[ASL 2.0](LICENSE)
