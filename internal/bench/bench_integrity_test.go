// SPDX-License-Identifier: MIT

package bench

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeResults(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "results.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// A row that does not parse must stop the summary rather than vanish from it,
// or a failed run drops out and the pass rate goes up.
func TestLoadResultsRejectsMalformedRow(t *testing.T) {
	p := writeResults(t,
		`{"task_id":"t1","arm":"A","verify_exit_code":0}`,
		`{"task_id":"t2","arm":"A","verify_ex`,
		`{"task_id":"t3","arm":"A","verify_exit_code":1}`,
	)
	_, err := LoadResults(p)
	if err == nil {
		t.Fatal("a truncated row was accepted")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name the bad line: %v", err)
	}
}

// The runner reads the same file to decide what is already done. Skipping a bad
// row would run that task again and append a duplicate.
func TestNewRunnerRefusesMalformedResults(t *testing.T) {
	p := writeResults(t, `not json`)
	if _, err := NewRunner(Config{ResultsFile: p, TracesDir: t.TempDir()}); err == nil {
		t.Fatal("NewRunner started over a results file it could not read")
	}
}

// A baryo binary that cannot be started is the harness failing, not the model.
// It must not be scored or cached as done.
func TestBaryoStartFailureIsInfraFailure(t *testing.T) {
	repo := t.TempDir()
	execCommand(t, repo, "git", "init", "-q")
	execCommand(t, repo, "git", "config", "user.name", "t")
	execCommand(t, repo, "git", "config", "user.email", "t@t")
	_ = os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0o644)
	execCommand(t, repo, "git", "add", "f.txt")
	execCommand(t, repo, "git", "commit", "-q", "-m", "init")
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(out))

	marker := filepath.Join(t.TempDir(), "verify-ran")
	results := filepath.Join(t.TempDir(), "results.jsonl")
	r, err := NewRunner(Config{
		BaryoBinary: filepath.Join(t.TempDir(), "no-such-baryo"),
		TracesDir:   t.TempDir(),
		ResultsFile: results,
		Arms:        []string{ArmA},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "t1", Repo: repo, StartCommit: sha, Prompt: "p", VerifyCommand: "touch " + marker}
	res, _ := r.runTaskArm(context.Background(), task, ArmA)
	if !res.InfraFailure {
		t.Errorf("start failure recorded as a model run: %+v", res)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("verify ran after baryo could not start, so the result would be scored")
	}
}
