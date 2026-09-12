package ignore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// tempRepo builds a git repo with a .gitignore and a .baryoignore and returns it.
func tempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v %s", err, out)
	}
	write := func(name, body string) {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "git-ignored.txt\nbuild/\n")
	write(".baryoignore", "secret-*.txt\n!secret-ok.txt\n")
	write("git-ignored.txt", "x")
	write("build/out.bin", "x")
	write("secret-a.txt", "x")
	write("secret-ok.txt", "x")
	write("keep.go", "package main")
	write(".env", "TOKEN=x")
	write("id.pem", "x")
	t.Chdir(dir)
	return dir
}

func paths(dir string, names ...string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(dir, n))
	}
	return out
}

// Filter must agree with IsIgnored on every path, or the hot loops that switch
// to it would start including files the single-path callers exclude.
func TestFilterAgreesWithIsIgnored(t *testing.T) {
	dir := tempRepo(t)
	all := paths(dir, ".env", "id.pem", "secret-a.txt", "secret-ok.txt",
		"git-ignored.txt", "build/out.bin", "keep.go")

	got := Filter(context.Background(), all)
	for _, p := range all {
		want := IsIgnored(context.Background(), p)
		if got[p] != want {
			t.Errorf("%s: Filter=%v IsIgnored=%v", filepath.Base(p), got[p], want)
		}
	}
}

func TestFilterClassifiesEachSource(t *testing.T) {
	dir := tempRepo(t)
	got := Filter(context.Background(), paths(dir, ".env", "id.pem", "secret-a.txt",
		"secret-ok.txt", "git-ignored.txt", "keep.go"))

	for _, c := range []struct {
		name string
		want bool
		why  string
	}{
		{".env", true, "builtin pattern"},
		{"id.pem", true, "builtin pattern"},
		{"secret-a.txt", true, ".baryoignore rule"},
		{"secret-ok.txt", false, ".baryoignore negation"},
		{"git-ignored.txt", true, "git check-ignore"},
		{"keep.go", false, "not ignored"},
	} {
		if got[filepath.Join(dir, c.name)] != c.want {
			t.Errorf("%s (%s): got %v, want %v", c.name, c.why, got[filepath.Join(dir, c.name)], c.want)
		}
	}
}

// The point of the change: one subprocess for the whole batch, not one per file.
func TestFilterSpawnsGitOnce(t *testing.T) {
	dir := tempRepo(t)

	calls := 0
	orig := gitCheckIgnore
	gitCheckIgnore = func(ctx context.Context, workDir string, in []string) map[string]bool {
		calls++
		return orig(ctx, workDir, in)
	}
	t.Cleanup(func() { gitCheckIgnore = orig })

	all := paths(dir, "keep.go", "git-ignored.txt", "build/out.bin", "secret-ok.txt")
	got := Filter(context.Background(), all)

	if calls != 1 {
		t.Errorf("git was spawned %d times for %d paths, want 1", calls, len(all))
	}
	if !got[filepath.Join(dir, "git-ignored.txt")] {
		t.Error("the batched call did not report the gitignored file")
	}
}

func TestFilterEmptyInput(t *testing.T) {
	tempRepo(t)
	calls := 0
	orig := gitCheckIgnore
	gitCheckIgnore = func(ctx context.Context, workDir string, in []string) map[string]bool {
		calls++
		return orig(ctx, workDir, in)
	}
	t.Cleanup(func() { gitCheckIgnore = orig })

	if got := Filter(context.Background(), nil); len(got) != 0 {
		t.Errorf("got %v, want an empty set", got)
	}
	if calls != 0 {
		t.Error("git should not be spawned for an empty batch")
	}
}
