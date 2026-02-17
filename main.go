package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

var baseDir = filepath.Join(os.Getenv("HOME"), "sandbox", "worktrees")

func main() {
	rootCmd := &cobra.Command{
		Use:   "wt",
		Short: "Git worktree manager",
		Long:  "A CLI tool to manage git worktrees in a centralized location.",
	}

	// Add command
	addCmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a new worktree",
		Args:  cobra.ExactArgs(1),
		RunE:  runAdd,
	}

	// List command
	lsCmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List all worktrees",
		Args:    cobra.NoArgs,
		RunE:    runList,
	}

	// Remove command
	rmCmd := &cobra.Command{
		Use:     "rm <name> [git-args...]",
		Aliases: []string{"remove"},
		Short:   "Remove a worktree",
		Args:    cobra.MinimumNArgs(1),
		RunE:    runRemove,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) != 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return getWorktreeNames(toComplete), cobra.ShellCompDirectiveNoFileComp
		},
	}
	rmCmd.Flags().SetInterspersed(false)

	worktreeArgsCompletion := func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return getWorktreeNames(toComplete), cobra.ShellCompDirectiveNoFileComp
	}

	// CD command
	cdCmd := &cobra.Command{
		Use:               "cd [name]",
		Short:             "Open a new shell in the worktree directory",
		Args:              cobra.MaximumNArgs(1),
		RunE:              runCD,
		ValidArgsFunction: worktreeArgsCompletion,
	}
	cdCmd.Flags().BoolP("create", "c", false, "Create worktree if it doesn't exist")

	// Code command
	codeCmd := &cobra.Command{
		Use:               "code [name]",
		Short:             "Open the worktree directory in VS Code",
		Args:              cobra.MaximumNArgs(1),
		RunE:              runCode,
		ValidArgsFunction: worktreeArgsCompletion,
	}
	codeCmd.Flags().BoolP("create", "c", false, "Create worktree if it doesn't exist")

	// Completion command
	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for wt.

To load completions:

Bash:
  $ source <(wt completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ wt completion bash > /etc/bash_completion.d/wt
  # macOS:
  $ wt completion bash > $(brew --prefix)/etc/bash_completion.d/wt

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ wt completion zsh > "${fpath[1]}/_wt"

  # You may need to start a new shell for this setup to take effect.

Fish:
  $ wt completion fish | source
  # To load completions for each session, execute once:
  $ wt completion fish > ~/.config/fish/completions/wt.fish

PowerShell:
  PS> wt completion powershell | Out-String | Invoke-Expression
  # To load completions for every new session, run:
  PS> wt completion powershell > wt.ps1
  # and source this file from your PowerShell profile.
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unknown shell: %s", args[0])
			}
		},
	}

	rootCmd.AddCommand(addCmd, lsCmd, rmCmd, cdCmd, codeCmd, completionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func getWorktreeNames(prefix string) []string {
	var names []string
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return names
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func runAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	worktreePath := filepath.Join(baseDir, name)
	projectDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Fetch latest from origin
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	if err := fetchCmd.Run(); err != nil {
		return fmt.Errorf("git fetch origin failed: %w", err)
	}

	// Create worktree off origin/main
	gitCmd := exec.Command("git", "worktree", "add", "--detach", worktreePath, "origin/main")
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	if err := gitCmd.Run(); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}

	// Copy .env if exists
	envSrc := filepath.Join(projectDir, ".env")
	if _, err := os.Stat(envSrc); err == nil {
		if err := copyFile(envSrc, filepath.Join(worktreePath, ".env")); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to copy .env: %v\n", err)
		}
	}

	// Copy .envrc if exists and run direnv allow
	envrcSrc := filepath.Join(projectDir, ".envrc")
	if _, err := os.Stat(envrcSrc); err == nil {
		if err := copyFile(envrcSrc, filepath.Join(worktreePath, ".envrc")); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to copy .envrc: %v\n", err)
		} else {
			direnvCmd := exec.Command("direnv", "allow")
			direnvCmd.Dir = worktreePath
			direnvCmd.Stdout = os.Stdout
			direnvCmd.Stderr = os.Stderr
			_ = direnvCmd.Run() // Ignore error if direnv not installed
		}
	}

	// Set up .devcontainer/.env if .devcontainer dir exists in the worktree
	devcontainerDir := filepath.Join(worktreePath, ".devcontainer")
	if _, err := os.Stat(devcontainerDir); err == nil {
		devEnvPath := filepath.Join(devcontainerDir, ".env")

		// Copy .devcontainer/.env from source project if it exists
		srcDevEnv := filepath.Join(projectDir, ".devcontainer", ".env")
		if _, err := os.Stat(srcDevEnv); err == nil {
			if err := copyFile(srcDevEnv, devEnvPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to copy .devcontainer/.env: %v\n", err)
			}
		}

		// Append worktree-specific env vars
		f, err := os.OpenFile(devEnvPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write .devcontainer/.env: %v\n", err)
		} else {
			fmt.Fprintf(f, "GIT_WORKTREE=%s\n", name)
			if port, err := findFreePort(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to find free port: %v\n", err)
			} else {
				fmt.Fprintf(f, "MICROSOCKS_PORT=%d\n", port)
			}
			f.Close()
		}
	}

	fmt.Println(worktreePath)
	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	gitCmd := exec.Command("git", "worktree", "list", "--porcelain")
	output, err := gitCmd.Output()
	if err != nil {
		return fmt.Errorf("git worktree list failed: %w", err)
	}

	prefix := "worktree " + baseDir + "/"
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, prefix) {
			fmt.Println(strings.TrimPrefix(line, prefix))
		}
	}
	return nil
}

func runRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	worktreePath := filepath.Join(baseDir, name)

	gitArgs := append([]string{"worktree", "remove", worktreePath}, args[1:]...)
	gitCmd := exec.Command("git", gitArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	return gitCmd.Run()
}

func resolveWorktreeDir(cmd *cobra.Command, args []string) (string, error) {
	create, _ := cmd.Flags().GetBool("create")

	if len(args) == 0 {
		// No name provided, go to main worktree
		gitCmd := exec.Command("git", "worktree", "list", "--porcelain")
		output, err := gitCmd.Output()
		if err != nil {
			return "", fmt.Errorf("git worktree list failed: %w", err)
		}
		for _, line := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(line, "worktree ") {
				return strings.TrimPrefix(line, "worktree "), nil
			}
		}
		return "", fmt.Errorf("no worktrees found")
	}

	name := args[0]
	dir := filepath.Join(baseDir, name)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if create {
			if err := runAdd(cmd, args); err != nil {
				return "", err
			}
		} else {
			if !confirmCreate(name) {
				return "", fmt.Errorf("aborted")
			}
			if err := runAdd(cmd, args); err != nil {
				return "", err
			}
		}
	}

	return dir, nil
}

func runCD(cmd *cobra.Command, args []string) error {
	dir, err := resolveWorktreeDir(cmd, args)
	if err != nil {
		return err
	}
	return execShellInDir(dir)
}

func runCode(cmd *cobra.Command, args []string) error {
	dir, err := resolveWorktreeDir(cmd, args)
	if err != nil {
		return err
	}

	var openCmd *exec.Cmd
	devcontainerJSON := filepath.Join(dir, ".devcontainer", "devcontainer.json")
	if _, err := os.Stat(devcontainerJSON); err == nil {
		if _, err := exec.LookPath("devcontainer"); err == nil {
			openCmd = exec.Command("devcontainer", "open-in-code", dir)
		}
	}
	if openCmd == nil {
		openCmd = exec.Command("code", dir)
	}

	openCmd.Stdout = os.Stdout
	openCmd.Stderr = os.Stderr
	return openCmd.Run()
}

func confirmCreate(name string) bool {
	fmt.Printf("Worktree '%s' doesn't exist. Create it now? [y/N] ", name)
	reader := bufio.NewReader(os.Stdin)
	reply, _ := reader.ReadString('\n')
	reply = strings.TrimSpace(strings.ToLower(reply))
	return reply == "y" || reply == "yes"
}

func findFreePort() (int, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func getParentShell() string {
	ppid := os.Getppid()
	// Use ps to get the parent process command name
	cmd := exec.Command("ps", "-p", fmt.Sprintf("%d", ppid), "-o", "comm=")
	output, err := cmd.Output()
	if err == nil {
		shell := strings.TrimSpace(string(output))
		// Login shells on macOS show as "-zsh" or "-bash", strip the leading hyphen
		shell = strings.TrimPrefix(shell, "-")
		if shell != "" {
			return shell
		}
	}
	// Fall back to SHELL environment variable
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	// Ultimate fallback
	return "/bin/sh"
}

func execShellInDir(dir string) error {
	shell := getParentShell()
	// If shell is just a name (e.g., "zsh"), find its full path
	shellPath, err := exec.LookPath(shell)
	if err != nil {
		return fmt.Errorf("failed to find shell %q: %w", shell, err)
	}

	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("failed to change to directory %q: %w", dir, err)
	}

	// Exec replaces the current process with the shell
	if err := syscall.Exec(shellPath, []string{shell}, os.Environ()); err != nil {
		return fmt.Errorf("failed to exec shell %q: %w", shellPath, err)
	}
	return nil
}
