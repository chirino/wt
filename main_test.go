package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunAddCreatesBranchBackedWorktree(t *testing.T) {
	root := setupTestRepository(t)
	cmd := testAddCommand(t, false)

	if err := runAdd(cmd, []string{"feature/accounts"}); err != nil {
		t.Fatalf("runAdd failed: %v", err)
	}

	worktreePath := filepath.Join(filepath.Dir(root), filepath.Base(root)+"@feature%2Faccounts")
	if got := gitOutput(t, worktreePath, "symbolic-ref", "--short", "HEAD"); got != "feature/accounts" {
		t.Fatalf("branch = %q, want %q", got, "feature/accounts")
	}
	resolved, ok, err := resolveWorktreeSelector("feature/accounts")
	if err != nil {
		t.Fatalf("resolveWorktreeSelector failed: %v", err)
	}
	if !ok || !samePath(resolved, worktreePath) {
		t.Fatalf("resolved = %q, %v; want %q, true", resolved, ok, worktreePath)
	}
	legacyResolved, ok, err := resolveWorktreeSelector("feature%2Faccounts")
	if err != nil {
		t.Fatalf("legacy alias resolution failed: %v", err)
	}
	if !ok || !samePath(legacyResolved, worktreePath) {
		t.Fatalf("legacy resolved = %q, %v; want %q, true", legacyResolved, ok, worktreePath)
	}
}

func TestRunAddCanCreateDetachedWorktree(t *testing.T) {
	root := setupTestRepository(t)
	cmd := testAddCommand(t, true)

	if err := runAdd(cmd, []string{"experiment"}); err != nil {
		t.Fatalf("runAdd failed: %v", err)
	}

	worktreePath := filepath.Join(filepath.Dir(root), filepath.Base(root)+"@experiment")
	symbolicRef := exec.Command("git", "symbolic-ref", "--quiet", "HEAD")
	symbolicRef.Dir = worktreePath
	if err := symbolicRef.Run(); err == nil {
		t.Fatal("detached worktree unexpectedly has an associated branch")
	}
	resolved, ok, err := resolveWorktreeSelector("experiment")
	if err != nil {
		t.Fatalf("resolveWorktreeSelector failed: %v", err)
	}
	if !ok || !samePath(resolved, worktreePath) {
		t.Fatalf("resolved = %q, %v; want %q, true", resolved, ok, worktreePath)
	}
}

func TestRunAddReusesExistingLocalBranch(t *testing.T) {
	root := setupTestRepository(t)
	gitRun(t, root, "branch", "existing")

	if err := runAdd(testAddCommand(t, false), []string{"existing"}); err != nil {
		t.Fatalf("runAdd failed: %v", err)
	}

	worktreePath := filepath.Join(filepath.Dir(root), filepath.Base(root)+"@existing")
	if got := gitOutput(t, worktreePath, "symbolic-ref", "--short", "HEAD"); got != "existing" {
		t.Fatalf("branch = %q, want %q", got, "existing")
	}
}

func TestBranchSelectorsSupportNonSiblingWorktrees(t *testing.T) {
	root := setupTestRepository(t)
	externalParent := filepath.Join(filepath.Dir(root), "codex", "c258")
	if err := os.MkdirAll(externalParent, 0o755); err != nil {
		t.Fatal(err)
	}
	externalPath := filepath.Join(externalParent, filepath.Base(root))
	gitRun(t, root, "worktree", "add", "-b", "add-ai-chart-of-accounts", externalPath, "HEAD")

	chdir(t, externalPath)
	if got, err := resolveCurrentWorktreeName(); err != nil || got != "add-ai-chart-of-accounts" {
		t.Fatalf("resolveCurrentWorktreeName() = %q, %v", got, err)
	}
	resolved, ok, err := resolveWorktreeSelector("add-ai-chart-of-accounts")
	if err != nil {
		t.Fatalf("resolveWorktreeSelector failed: %v", err)
	}
	if !ok || !samePath(resolved, externalPath) {
		t.Fatalf("resolved = %q, %v; want %q, true", resolved, ok, externalPath)
	}
	dir, remaining, err := resolveWorkspaceFolder([]string{"add-ai-chart-of-accounts", "echo", "ok"})
	if err != nil {
		t.Fatalf("resolveWorkspaceFolder failed: %v", err)
	}
	if !samePath(dir, externalPath) || !reflect.DeepEqual(remaining, []string{"echo", "ok"}) {
		t.Fatalf("resolveWorkspaceFolder = %q, %q; want %q, %q", dir, remaining, externalPath, []string{"echo", "ok"})
	}
	if names := getWorktreeNames(""); !contains(names, "add-ai-chart-of-accounts") {
		t.Fatalf("completion names %q do not contain branch", names)
	}
}

func TestLegacySiblingAliasStillResolves(t *testing.T) {
	root := setupTestRepository(t)
	worktreePath := filepath.Join(filepath.Dir(root), filepath.Base(root)+"@review")
	gitRun(t, root, "worktree", "add", "--detach", worktreePath, "HEAD")

	resolved, ok, err := resolveWorktreeSelector("review")
	if err != nil {
		t.Fatalf("resolveWorktreeSelector failed: %v", err)
	}
	if !ok || !samePath(resolved, worktreePath) {
		t.Fatalf("resolved = %q, %v; want %q, true", resolved, ok, worktreePath)
	}
}

func setupTestRepository(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "README.md")
	gitRun(t, root, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	chdir(t, root)
	return root
}

func testAddCommand(t *testing.T, detach bool) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().Bool("detach", false, "")
	if err := cmd.Flags().Set("detach", strconv.FormatBool(detach)); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func samePath(left, right string) bool {
	return normalizePathForCompare(left) == normalizePathForCompare(right)
}
