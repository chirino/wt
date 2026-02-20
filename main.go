package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

//go:embed SKILL.md
var wtExecSkill string

//go:embed devcontainer/devcontainer.json
var initDevcontainerJSON string

//go:embed devcontainer/Dockerfile
var initDockerfile string

//go:embed devcontainer/supervisord.conf
var initSupervisordConf string

const worktreeDelimiter = "@"

var verbose bool

func main() {
	rootCmd := &cobra.Command{
		Use:   "wt",
		Short: "Git worktree manager",
		Long:  "A CLI tool to manage git worktrees as siblings of the main repository.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return nil
		},
	}
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")

	// Add command
	addCmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a new worktree (sibling of main repo, e.g. repo@name)",
		Args:  cobra.ExactArgs(1),
		RunE:  runAdd,
	}

	// List command
	lsCmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List all sibling worktrees",
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

	// Name command
	nameCmd := &cobra.Command{
		Use:   "name",
		Short: "Print the name of the current worktree",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveCurrentWorktreeName()
			if err != nil {
				return err
			}
			fmt.Println(name)
			return nil
		},
	}

	// Dir command
	dirCmd := &cobra.Command{
		Use:   "dir",
		Short: "Print the root directory of the current worktree or git project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := getCurrentWorktreeRoot()
			if err != nil {
				return fmt.Errorf("not in a git repository")
			}
			fmt.Println(root)
			return nil
		},
	}

	// Exec command
	execCmd := &cobra.Command{
		Use:               "exec [name] -- <command> [args...]",
		Short:             "Execute a command in the worktree's devcontainer (default: current worktree)",
		Args:              cobra.ArbitraryArgs,
		RunE:              runExec,
		ValidArgsFunction: worktreeArgsCompletion,
	}
	execCmd.Flags().SetInterspersed(false)

	// Up command
	upCmd := &cobra.Command{
		Use:               "up [name] [devcontainer-args...]",
		Short:             "Start the worktree's devcontainer (default: current worktree)",
		Args:              cobra.ArbitraryArgs,
		RunE:              runUp,
		ValidArgsFunction: worktreeArgsCompletion,
	}
	upCmd.Flags().SetInterspersed(false)

	// Build command
	buildCmd := &cobra.Command{
		Use:               "build [name] [devcontainer-args...]",
		Short:             "Build the worktree's devcontainer (default: current worktree)",
		Args:              cobra.ArbitraryArgs,
		RunE:              runBuild,
		ValidArgsFunction: worktreeArgsCompletion,
	}
	buildCmd.Flags().SetInterspersed(false)

	// Proxy-port command
	proxyPortCmd := &cobra.Command{
		Use:               "proxy-port [name]",
		Short:             "Print the SOCKS proxy port for the worktree's devcontainer",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: worktreeArgsCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _, err := resolveWorkspaceFolder(args)
			if err != nil {
				return err
			}
			port, err := getProxyPort(dir)
			if err != nil {
				return err
			}
			fmt.Println(port)
			return nil
		},
	}

	// Skill command
	skillCmd := &cobra.Command{
		Use:   "skill",
		Short: "Print the Claude Code skill for worktree-isolated execution",
		Long: `Print a Claude Code skill file that teaches Claude to use wt exec
for commands that could conflict across worktrees.

To import into a project:
  wt skill > .claude/wt-exec.md

Then add to the project's CLAUDE.md:
  @.claude/wt-exec.md`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(wtExecSkill)
		},
	}

	// Chrome command
	chromeCmd := &cobra.Command{
		Use:               "chrome [name] [-- chrome-args...]",
		Short:             "Open a Chrome browser with per-worktree profile and proxy settings",
		Args:              cobra.ArbitraryArgs,
		RunE:              runChrome,
		ValidArgsFunction: worktreeArgsCompletion,
	}
	chromeCmd.Flags().SetInterspersed(false)

	// Playwright command
	playwrightCmd := &cobra.Command{
		Use:               "playwright [name] [-- playwright-args...]",
		Short:             "Open a browser with Playwright using per-worktree proxy settings",
		Args:              cobra.ArbitraryArgs,
		RunE:              runPlaywright,
		ValidArgsFunction: worktreeArgsCompletion,
	}
	playwrightCmd.Flags().SetInterspersed(false)

	// Curl command
	curlCmd := &cobra.Command{
		Use:               "curl [name] [-- curl-args...]",
		Short:             "Run curl with per-worktree proxy settings",
		Args:              cobra.ArbitraryArgs,
		RunE:              runCurl,
		ValidArgsFunction: worktreeArgsCompletion,
	}
	curlCmd.Flags().SetInterspersed(false)

	// Init command
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create a minimal .devcontainer/ with SOCKS5 proxy support",
		Args:  cobra.NoArgs,
		RunE:  runInit,
	}
	initCmd.Flags().Bool("force", false, "overwrite existing .devcontainer/ files")

	// Down command
	downCmd := &cobra.Command{
		Use:               "down [name]",
		Short:             "Stop and remove the devcontainer for a worktree",
		Args:              cobra.MaximumNArgs(1),
		RunE:              runDown,
		ValidArgsFunction: worktreeArgsCompletion,
	}

	rootCmd.AddCommand(addCmd, lsCmd, rmCmd, cdCmd, codeCmd, chromeCmd, playwrightCmd, curlCmd, nameCmd, dirCmd, execCmd, upCmd, downCmd, buildCmd, proxyPortCmd, skillCmd, completionCmd, initCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// getMainRepoRoot returns the absolute path to the main repository root.
// Works from the main repo, any worktree, or any subdirectory thereof.
func getMainRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %w", err)
	}
	commonDir := strings.TrimSpace(string(output))
	if !filepath.IsAbs(commonDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		commonDir = filepath.Join(cwd, commonDir)
	}
	return filepath.Dir(filepath.Clean(commonDir)), nil
}

// getWorktreeParentDir returns the parent directory where sibling worktrees live.
func getWorktreeParentDir() (string, error) {
	mainRoot, err := getMainRepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Dir(mainRoot), nil
}

// getCurrentWorktreeRoot returns the toplevel of the current working tree.
func getCurrentWorktreeRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// worktreeDirName returns the directory name for a worktree: "repo@name".
func worktreeDirName(repoBasename, name string) string {
	return repoBasename + worktreeDelimiter + name
}

// parseWorktreeName extracts the worktree name from a directory name like "repo@name".
// Returns empty string if the directory doesn't match the repo prefix.
func parseWorktreeName(dirName, repoBasename string) string {
	prefix := repoBasename + worktreeDelimiter
	if strings.HasPrefix(dirName, prefix) {
		return strings.TrimPrefix(dirName, prefix)
	}
	return ""
}

// resolveCurrentWorktreeName returns the name of the current worktree based on cwd.
// Returns an error if the user is not inside a named worktree.
func resolveCurrentWorktreeName() (string, error) {
	wtRoot, err := getCurrentWorktreeRoot()
	if err != nil {
		return "", fmt.Errorf("not in a git worktree")
	}
	mainRoot, err := getMainRepoRoot()
	if err != nil {
		return "", err
	}
	if wtRoot == mainRoot {
		return "", fmt.Errorf("currently in the main worktree, not a named worktree")
	}
	repoBasename := filepath.Base(mainRoot)
	name := parseWorktreeName(filepath.Base(wtRoot), repoBasename)
	if name == "" {
		return "", fmt.Errorf("current directory is not in a recognized worktree")
	}
	return name, nil
}

// resolveNameArg resolves a name argument, treating "." as the current worktree.
func resolveNameArg(name string) (string, error) {
	if name == "." {
		return resolveCurrentWorktreeName()
	}
	if err := validateWorktreeName(name); err != nil {
		return "", err
	}
	return name, nil
}

// resolveWorktreePath returns the full path for a worktree by name.
func resolveWorktreePath(name string) (string, error) {
	if err := validateWorktreeName(name); err != nil {
		return "", err
	}
	parentDir, err := getWorktreeParentDir()
	if err != nil {
		return "", err
	}
	mainRoot, err := getMainRepoRoot()
	if err != nil {
		return "", err
	}
	dirName := worktreeDirName(filepath.Base(mainRoot), name)
	return filepath.Join(parentDir, dirName), nil
}

func getWorktreeNames(prefix string) []string {
	mainRoot, err := getMainRepoRoot()
	if err != nil {
		return nil
	}
	parentDir := filepath.Dir(mainRoot)
	repoBasename := filepath.Base(mainRoot)

	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var names []string
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		wtPath := strings.TrimPrefix(line, "worktree ")
		if wtPath == mainRoot {
			continue
		}
		if filepath.Dir(wtPath) != parentDir {
			continue
		}
		name := parseWorktreeName(filepath.Base(wtPath), repoBasename)
		if name != "" && strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return names
}

func runAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := validateWorktreeName(name); err != nil {
		return err
	}

	worktreePath, err := resolveWorktreePath(name)
	if err != nil {
		return err
	}

	// Check if target path already exists
	if info, err := os.Stat(worktreePath); err == nil {
		if info.IsDir() {
			gitPath := filepath.Join(worktreePath, ".git")
			if _, err := os.Stat(gitPath); err == nil {
				return fmt.Errorf("'%s' already exists with a .git entry; choose a different name or remove it first", filepath.Base(worktreePath))
			}
			return fmt.Errorf("'%s' already exists but is not a git worktree; choose a different name or remove it first", filepath.Base(worktreePath))
		}
		return fmt.Errorf("'%s' already exists as a file; choose a different name or remove it first", filepath.Base(worktreePath))
	}

	// Determine source directory for copying config files
	projectDir, err := getCurrentWorktreeRoot()
	if err != nil {
		projectDir, _ = os.Getwd()
	}

	// Ensure relative paths for worktree links (devcontainer compatibility)
	_ = exec.Command("git", "config", "worktree.useRelativePaths", "true").Run()

	// Best-effort fetch from origin, if configured.
	if err := exec.Command("git", "remote", "get-url", "origin").Run(); err == nil {
		fetchCmd := exec.Command("git", "fetch", "origin")
		fetchCmd.Stdout = os.Stdout
		fetchCmd.Stderr = os.Stderr
		if err := fetchCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: git fetch origin failed: %v\n", err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "Warning: git remote 'origin' not configured; skipping fetch")
	}

	// Create worktree off current HEAD
	gitCmd := exec.Command("git", "worktree", "add", "--detach", worktreePath, "HEAD")
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
			f.Close()
		}
	}

	fmt.Println(worktreePath)
	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	mainRoot, err := getMainRepoRoot()
	if err != nil {
		return err
	}
	parentDir := filepath.Dir(mainRoot)
	repoBasename := filepath.Base(mainRoot)

	gitCmd := exec.Command("git", "worktree", "list", "--porcelain")
	output, err := gitCmd.Output()
	if err != nil {
		return fmt.Errorf("git worktree list failed: %w", err)
	}

	for _, line := range strings.Split(string(output), "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		wtPath := strings.TrimPrefix(line, "worktree ")
		if wtPath == mainRoot {
			continue
		}
		if filepath.Dir(wtPath) != parentDir {
			continue
		}
		name := parseWorktreeName(filepath.Base(wtPath), repoBasename)
		if name != "" {
			fmt.Println(name)
		}
	}
	return nil
}

func runRemove(cmd *cobra.Command, args []string) error {
	name, err := resolveNameArg(args[0])
	if err != nil {
		return err
	}
	worktreePath, err := resolveWorktreePath(name)
	if err != nil {
		return err
	}

	gitArgs := append([]string{"worktree", "remove", worktreePath}, args[1:]...)
	gitCmd := exec.Command("git", gitArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	if err := gitCmd.Run(); err != nil {
		return err
	}

	// Clean up any leftover files (e.g. .vscode-profile, untracked files)
	if _, err := os.Stat(worktreePath); err == nil {
		if err := os.RemoveAll(worktreePath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove %s: %v\n", worktreePath, err)
		}
	}
	return nil
}

func resolveWorktreeDir(cmd *cobra.Command, args []string) (string, error) {
	create, _ := cmd.Flags().GetBool("create")

	if len(args) == 0 {
		// No name provided, go to main repo root
		return getMainRepoRoot()
	}

	name, err := resolveNameArg(args[0])
	if err != nil {
		return "", err
	}
	dir, err := resolveWorktreePath(name)
	if err != nil {
		return "", err
	}

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

	devcontainerJSON := filepath.Join(dir, ".devcontainer", "devcontainer.json")
	if _, err := os.Stat(devcontainerJSON); err == nil {
		if _, err := exec.LookPath("devcontainer"); err == nil {
			return openDevcontainer(dir)
		}
	}

	return sysExec("code", []string{dir})
}

func findChromeBinary() (string, error) {
	// Check common names in PATH first
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium-browser", "chromium"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	// macOS application bundle
	if runtime.GOOS == "darwin" {
		macPath := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(macPath); err == nil {
			return macPath, nil
		}
	}
	return "", fmt.Errorf("could not find Chrome or Chromium; install Google Chrome or add it to your PATH")
}

func runChrome(cmd *cobra.Command, args []string) error {
	dir, extra, err := resolveWorkspaceFolder(args)
	if err != nil {
		return err
	}

	chromeBin, err := findChromeBinary()
	if err != nil {
		return err
	}

	profileDir := filepath.Join(dir, ".chrome-profile")
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return fmt.Errorf("failed to create Chrome profile directory: %w", err)
	}

	chromeArgs := []string{
		"--user-data-dir=" + profileDir,
		// Skip onboarding UI in fresh profiles.
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-features=ChromeSignin",
	}

	// Require a proxy port so all traffic is forced through it.
	port, err := getProxyPort(dir)
	if err != nil {
		return err
	}
	chromeArgs = append(chromeArgs, "--proxy-server=socks5://127.0.0.1:"+port)
	// Proxy everything, including loopback targets, through SOCKS.
	chromeArgs = append(chromeArgs, "--proxy-bypass-list=<-loopback>")

	for i, arg := range extra {
		extra[i] = normalizeLocalhostURL(arg)
	}
	chromeArgs = append(chromeArgs, extra...)

	chromeCmd := exec.Command(chromeBin, chromeArgs...)
	if verbose {
		quotedArgs := make([]string, len(chromeArgs))
		for i, arg := range chromeArgs {
			quotedArgs[i] = strconv.Quote(arg)
		}
		fmt.Fprintf(os.Stderr, "Launching Chrome: %s %s\n", strconv.Quote(chromeBin), strings.Join(quotedArgs, " "))
		chromeCmd.Stdout = os.Stdout
		chromeCmd.Stderr = os.Stderr
	}
	return chromeCmd.Start()
}

func runPlaywright(cmd *cobra.Command, args []string) error {
	dir, extra, err := resolveWorkspaceFolder(args)
	if err != nil {
		return err
	}

	npx, err := exec.LookPath("npx")
	if err != nil {
		return fmt.Errorf("could not find npx; install Node.js and Playwright")
	}

	// Require a proxy port so all traffic is forced through it.
	port, err := getProxyPort(dir)
	if err != nil {
		return err
	}

	for i, arg := range extra {
		extra[i] = normalizeLocalhostURL(arg)
	}

	playwrightArgs := []string{
		"playwright",
		"open",
		"--proxy-server=socks5://127.0.0.1:" + port,
	}
	playwrightArgs = append(playwrightArgs, extra...)

	playwrightCmd := exec.Command(npx, playwrightArgs...)
	if verbose {
		quotedArgs := make([]string, len(playwrightArgs))
		for i, arg := range playwrightArgs {
			quotedArgs[i] = strconv.Quote(arg)
		}
		fmt.Fprintf(os.Stderr, "Launching Playwright: %s %s\n", strconv.Quote(npx), strings.Join(quotedArgs, " "))
		playwrightCmd.Stdout = os.Stdout
		playwrightCmd.Stderr = os.Stderr
	}
	return playwrightCmd.Start()
}

func runCurl(cmd *cobra.Command, args []string) error {
	dir, extra, err := resolveWorkspaceFolder(args)
	if err != nil {
		return err
	}

	curlBin, err := exec.LookPath("curl")
	if err != nil {
		return fmt.Errorf("could not find curl; install curl first")
	}

	// Require a proxy port so all traffic is forced through it.
	port, err := getProxyPort(dir)
	if err != nil {
		return err
	}

	for i, arg := range extra {
		extra[i] = normalizeLocalhostURL(arg)
	}

	curlArgs := []string{
		"--proxy", "socks5h://127.0.0.1:" + port,
		"--noproxy", "",
	}
	curlArgs = append(curlArgs, extra...)

	curlCmd := exec.Command(curlBin, curlArgs...)
	if verbose {
		quotedArgs := make([]string, len(curlArgs))
		for i, arg := range curlArgs {
			quotedArgs[i] = strconv.Quote(arg)
		}
		fmt.Fprintf(os.Stderr, "Launching curl: %s %s\n", strconv.Quote(curlBin), strings.Join(quotedArgs, " "))
	}
	curlCmd.Stdout = os.Stdout
	curlCmd.Stderr = os.Stderr
	return curlCmd.Run()
}

func normalizeLocalhostURL(arg string) string {
	parsed, err := url.Parse(arg)
	if err != nil || parsed.Host == "" || parsed.Hostname() != "localhost" {
		return arg
	}
	if parsed.Port() == "" {
		parsed.Host = "127.0.0.1"
	} else {
		parsed.Host = net.JoinHostPort("127.0.0.1", parsed.Port())
	}
	return parsed.String()
}


func runExec(cmd *cobra.Command, args []string) error {
	dir, cmdArgs, err := resolveWorkspaceFolder(args)
	if err != nil {
		return err
	}
	devcontainerJSON := filepath.Join(dir, ".devcontainer", "devcontainer.json")
	if _, err := os.Stat(devcontainerJSON); err == nil {
		if len(cmdArgs) == 0 {
			cmdArgs = []string{"/bin/sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh"}
		}
		dcArgs := append([]string{"exec", "--workspace-folder", dir}, cmdArgs...)
		os.Setenv("DOCKER_CLI_HINTS", "false")
		return sysExec("devcontainer", dcArgs)
	}

	// No devcontainer config — run the command directly in the worktree
	if len(cmdArgs) == 0 {
		return execShellInDir(dir)
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("failed to change to directory %q: %w", dir, err)
	}
	return sysExec(cmdArgs[0], cmdArgs[1:])
}

// resolveExecArgs splits args into (worktreeName, commandArgs).
// If the first arg is "." or matches a known worktree name, it's used as the
// worktree name and the rest are the command. Otherwise, the current worktree
// is used and all args are treated as the command.
func resolveExecArgs(args []string) (string, []string, error) {
	if args[0] == "." {
		name, err := resolveCurrentWorktreeName()
		if err != nil {
			return "", nil, err
		}
		return name, args[1:], nil
	}

	// Check if the first arg is a known worktree name
	names := getWorktreeNames("")
	for _, n := range names {
		if args[0] == n {
			return n, args[1:], nil
		}
	}

	// First arg is not a worktree — default to current worktree
	name, err := resolveCurrentWorktreeName()
	if err != nil {
		return "", nil, err
	}
	return name, args, nil
}

func runUp(cmd *cobra.Command, args []string) error {
	dir, extra, err := resolveWorkspaceFolder(args)
	if err != nil {
		return err
	}
	dcArgs := append([]string{"up", "--workspace-folder", dir}, extra...)
	return sysExec("devcontainer", dcArgs)
}

func runDown(cmd *cobra.Command, args []string) error {
	dir, _, err := resolveWorkspaceFolder(args)
	if err != nil {
		return err
	}

	// Find the container by devcontainer label
	out, err := exec.Command("docker", "ps", "-aq", "--filter", "label=devcontainer.local_folder="+dir).Output()
	if err != nil {
		return fmt.Errorf("failed to query docker: %w", err)
	}
	containerID := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if containerID == "" {
		return fmt.Errorf("no devcontainer found for %q", filepath.Base(dir))
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Removing container %s\n", containerID)
	}
	rmCmd := exec.Command("docker", "rm", "-f", containerID)
	rmCmd.Stdout = os.Stdout
	rmCmd.Stderr = os.Stderr
	return rmCmd.Run()
}

func runBuild(cmd *cobra.Command, args []string) error {
	dir, extra, err := resolveWorkspaceFolder(args)
	if err != nil {
		return err
	}
	dcArgs := append([]string{"build", "--workspace-folder", dir}, extra...)
	return sysExec("devcontainer", dcArgs)
}

func runInit(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	devcontainerDir := filepath.Join(cwd, ".devcontainer")

	if info, err := os.Stat(devcontainerDir); err == nil && info.IsDir() {
		if !force {
			return fmt.Errorf(".devcontainer/ already exists; use --force to overwrite")
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "Overwriting existing .devcontainer/ directory\n")
		}
	}

	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		return fmt.Errorf("failed to create .devcontainer/: %w", err)
	}

	type templateFile struct {
		name    string
		content string
		perm    os.FileMode
	}
	files := []templateFile{
		{"devcontainer.json", initDevcontainerJSON, 0644},
		{"Dockerfile", initDockerfile, 0644},
		{"supervisord.conf", initSupervisordConf, 0644},
	}

	for _, f := range files {
		path := filepath.Join(devcontainerDir, f.name)
		if verbose {
			fmt.Fprintf(os.Stderr, "Writing .devcontainer/%s\n", f.name)
		}
		if err := os.WriteFile(path, []byte(f.content), f.perm); err != nil {
			return fmt.Errorf("failed to write %s: %w", f.name, err)
		}
	}

	fmt.Printf("Created .devcontainer/ in %s\n", cwd)
	return nil
}

// resolveWorkspaceFolder resolves args to a workspace directory and remaining args.
// Supports named worktrees and falls back to the main worktree when in it.
func resolveWorkspaceFolder(args []string) (string, []string, error) {
	name, extra, err := resolveOptionalWorktreeArgs(args)
	if err == nil {
		dir, err := resolveWorktreePath(name)
		return dir, extra, err
	}
	// Fall back to main worktree
	mainRoot, mainErr := getMainRepoRoot()
	if mainErr != nil {
		return "", nil, err
	}
	wtRoot, wtErr := getCurrentWorktreeRoot()
	if wtErr != nil || wtRoot != mainRoot {
		return "", nil, err
	}
	return mainRoot, args, nil
}

// resolveOptionalWorktreeArgs splits args into (worktreeName, remainingArgs).
// If the first arg is "." or matches a known worktree name, it's used as the
// worktree name. Otherwise, the current worktree is used and all args pass through.
func resolveOptionalWorktreeArgs(args []string) (string, []string, error) {
	if len(args) == 0 {
		name, err := resolveCurrentWorktreeName()
		if err != nil {
			return "", nil, err
		}
		return name, nil, nil
	}
	return resolveExecArgs(args)
}

func defaultVSCodeUserDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code")
	case "linux":
		return filepath.Join(home, ".config", "Code")
	default:
		return ""
	}
}

func defaultVSCodeExtensionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".vscode", "extensions")
}

func setupVSCodeProfile(userDataDir string) {
	defaultDataDir := defaultVSCodeUserDataDir()
	if defaultDataDir == "" {
		return
	}
	defaultUserDir := filepath.Join(defaultDataDir, "User")
	if _, err := os.Stat(defaultUserDir); err != nil {
		return
	}
	if err := os.MkdirAll(userDataDir, 0755); err != nil {
		return
	}
	symlinkPath := filepath.Join(userDataDir, "User")
	if _, err := os.Lstat(symlinkPath); os.IsNotExist(err) {
		_ = os.Symlink(defaultUserDir, symlinkPath)
	}
}

func openDevcontainer(dir string) error {
	// Start the devcontainer, streaming output while capturing it for JSON parsing
	var buf bytes.Buffer
	upCmd := exec.Command("devcontainer", "up", "--workspace-folder", dir)
	upCmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	upCmd.Stderr = os.Stderr
	if err := upCmd.Run(); err != nil {
		return fmt.Errorf("devcontainer up failed: %w", err)
	}
	out := buf.Bytes()

	// devcontainer up may mix progress text with JSON on stdout;
	// find the last line that looks like a JSON object
	var jsonLine []byte
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "{") {
			jsonLine = []byte(trimmed)
		}
	}
	if jsonLine == nil {
		return fmt.Errorf("devcontainer up produced no JSON output")
	}

	var result struct {
		ContainerID           string `json:"containerId"`
		RemoteWorkspaceFolder string `json:"remoteWorkspaceFolder"`
	}
	if err := json.Unmarshal(jsonLine, &result); err != nil {
		return fmt.Errorf("failed to parse devcontainer up output: %w", err)
	}

	// Build VS Code arguments
	hexID := hex.EncodeToString([]byte(result.ContainerID))
	folderURI := fmt.Sprintf("vscode-remote://attached-container+%s%s", hexID, result.RemoteWorkspaceFolder)

	userDataDir := filepath.Join(dir, ".vscode-profile")
	setupVSCodeProfile(userDataDir)

	codeArgs := []string{
		"--user-data-dir", userDataDir,
		"--folder-uri", folderURI,
	}

	// Share extensions from default VS Code installation
	defaultExtDir := defaultVSCodeExtensionsDir()
	if defaultExtDir != "" {
		if _, err := os.Stat(defaultExtDir); err == nil {
			codeArgs = append(codeArgs, "--extensions-dir", defaultExtDir)
		}
	}

	// Add proxy setting if devcontainer is running with a proxy port
	port, err := getProxyPort(dir)
	if err == nil {
		codeArgs = append(codeArgs, "--proxy-server=socks5://127.0.0.1:"+port)
	}

	return sysExec("code", codeArgs)
}

// getProxyPort discovers the host port mapped to the SOCKS5 proxy (container port 1080)
// by inspecting the running devcontainer for the given workspace directory.
func getProxyPort(dir string) (string, error) {
	out, err := exec.Command("docker", "ps", "-q", "--filter", "label=devcontainer.local_folder="+dir).Output()
	if err != nil {
		return "", fmt.Errorf("failed to query docker: %w", err)
	}
	containerID := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if containerID == "" {
		return "", fmt.Errorf("no running devcontainer found for %q", filepath.Base(dir))
	}

	out, err = exec.Command("docker", "port", containerID, "1080").Output()
	if err != nil {
		return "", fmt.Errorf("no proxy port mapped for devcontainer %q", filepath.Base(dir))
	}
	// Output format: "0.0.0.0:32768\n[::]:32768\n" — take the first line
	addr := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("failed to parse port from %q: %w", addr, err)
	}
	return port, nil
}

func validateWorktreeName(name string) error {
	if name == "" {
		return fmt.Errorf("worktree name cannot be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid worktree name %q", name)
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("invalid worktree name %q: path separators are not allowed", name)
	}
	if filepath.Base(name) != name || filepath.IsAbs(name) {
		return fmt.Errorf("invalid worktree name %q", name)
	}
	return nil
}

func confirmCreate(name string) bool {
	fmt.Printf("Worktree '%s' doesn't exist. Create it now? [y/N] ", name)
	reader := bufio.NewReader(os.Stdin)
	reply, _ := reader.ReadString('\n')
	reply = strings.TrimSpace(strings.ToLower(reply))
	return reply == "y" || reply == "yes"
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

func sysExec(argv0 string, args []string) error {
	path, err := exec.LookPath(argv0)
	if err != nil {
		return fmt.Errorf("failed to find %q: %w", argv0, err)
	}
	return syscall.Exec(path, append([]string{argv0}, args...), os.Environ())
}

func execShellInDir(dir string) error {
	shell := getParentShell()
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("failed to change to directory %q: %w", dir, err)
	}
	return sysExec(shell, nil)
}
