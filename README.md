# wt - Git Worktree Manager

A CLI tool that simplifies managing Git worktrees as sibling directories. It wraps Git's native `git worktree` command with devcontainer integration, making it easy to work on multiple branches simultaneously with full isolation.

## Why use wt?

Git worktrees let you check out multiple branches at the same time, each in its own directory. This is useful when you need to:

- **Work on multiple features or bug fixes in parallel** without stashing or committing incomplete work
- **Run long builds or tests on one branch** while continuing development on another
- **Review a pull request** without disrupting your current working directory
- **Compare behavior across branches** side by side

`wt` makes worktrees easy to manage and adds devcontainer support so each worktree can run in its own isolated container with its own network, ports, and services.

## Features

- **Sibling directory layout** - Worktrees are created as siblings of the main repo (e.g., `myproject@feature` next to `myproject/`)
- **Environment file copying** - Automatically copies `.env`, `.envrc`, and `.devcontainer/.env` to new worktrees
- **Direnv integration** - Runs `direnv allow` when `.envrc` is present
- **Devcontainer support** - Start, build, and exec into per-worktree devcontainers
- **VS Code integration** - Open worktrees in VS Code with devcontainer attach and per-worktree profile isolation
- **SOCKS proxy per worktree** - Each devcontainer gets a dedicated proxy port for accessing container services from the host
- **Shell navigation** - Quickly open a shell in any worktree
- **Shell completion** - Tab completion for bash, zsh, fish, and PowerShell
- **Claude Code skill** - Built-in skill file for AI-assisted development with worktree isolation

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
- Copies `.env` and `.envrc` from the current project
- Copies `.devcontainer/.env` if present
- Assigns a free port as `MICROSOCKS_PORT` for the devcontainer proxy
- Sets `GIT_WORKTREE=feature-xyz` in the devcontainer environment

### List worktrees

```bash
wt ls
```

### Navigate to a worktree

```bash
wt cd feature-xyz
```

Opens a new shell in the worktree directory. Without arguments, opens a shell in the main repo root.

Use `-c` to auto-create the worktree if it doesn't exist:

```bash
wt cd -c new-feature
```

### Open in VS Code

```bash
wt code feature-xyz
```

If the worktree has a `.devcontainer/devcontainer.json`, this will:
1. Run `devcontainer up` to start the container
2. Open VS Code attached to the running container
3. Use a per-worktree VS Code profile (`.vscode-profile/`) to avoid settings conflicts
4. Configure the SOCKS proxy for VS Code's network access

Without a devcontainer, it opens the directory in VS Code directly. Use `-c` to auto-create.

### Devcontainer commands

Start a worktree's devcontainer:

```bash
wt up feature-xyz
```

Build a worktree's devcontainer:

```bash
wt build feature-xyz
```

Run a command inside the devcontainer:

```bash
wt exec feature-xyz -- make test
wt exec feature-xyz -- npm run dev
```

Use `.` to refer to the current worktree:

```bash
wt exec . -- go test ./...
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

### Claude Code skill

Generate a skill file that teaches Claude Code to use `wt exec` for commands that could conflict across worktrees:

```bash
wt skill > .claude/wt-exec.md
```

Then reference it from your project's `CLAUDE.md`:

```
@.claude/wt-exec.md
```

## Command reference

| Command | Description |
|---|---|
| `wt add <name>` | Create a new worktree |
| `wt ls` | List all sibling worktrees |
| `wt cd [name]` | Open a shell in the worktree directory |
| `wt code [name]` | Open the worktree in VS Code |
| `wt rm <name>` | Remove a worktree |
| `wt up <name>` | Start the worktree's devcontainer |
| `wt build <name>` | Build the worktree's devcontainer |
| `wt exec <name> -- <cmd>` | Run a command in the worktree's devcontainer |
| `wt proxy-port [name]` | Print the SOCKS proxy port |
| `wt name` | Print the current worktree name |
| `wt dir` | Print the current worktree root directory |
| `wt skill` | Print the Claude Code skill file |
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
