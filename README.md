# wt - Git Worktree Manager

A CLI tool that simplifies managing Git worktrees in a centralized location. It wraps Git's native `git worktree` command, providing an easier interface for developers who frequently use worktrees for managing multiple branches or features simultaneously.

## Features

- **Centralized worktree management** - All worktrees are stored in `~/sandbox/worktrees/<name>`
- **Environment file copying** - Automatically copies `.env` and `.envrc` files to new worktrees
- **Direnv integration** - Runs `direnv allow` when `.envrc` is present
- **Shell integration** - Quickly navigate to worktrees with a new shell session
- **Shell completion** - Tab completion support for bash, zsh, fish, and PowerShell

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

### Add a new worktree

```bash
wt add feature-xyz
```

Creates a worktree at `~/sandbox/worktrees/feature-xyz` with a branch named `worktree/feature-xyz`. Copies `.env` and `.envrc` files from the parent project if they exist.

### List worktrees

```bash
wt ls
# or
wt list
```

### Change to a worktree

```bash
wt cd feature-xyz
```

Opens a new shell in the worktree directory. Without arguments, opens a shell in the main worktree.

Use the `-c` or `--create` flag to auto-create the worktree if it doesn't exist:

```bash
wt cd -c new-feature
```

### Remove a worktree

```bash
wt rm feature-xyz
# or
wt remove feature-xyz
```

## Shell Completion

Generate completion scripts for your shell:

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
