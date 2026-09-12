// SPDX-License-Identifier: MIT

package bench

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadTasks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// 1. Test JSON array format
	jsonArrayFile := filepath.Join(tmpDir, "tasks.json")
	jsonArrayData := `[
		{
			"id": "task-01",
			"repo": "/path/to/repo",
			"start_commit": "abc1234",
			"prompt": "Fix bug in parser",
			"verify": "go test ./parser",
			"repeat_shaped": true
		},
		{
			"id": "task-02",
			"repo": "/path/to/repo",
			"start_commit": "def5678",
			"prompt": "Add feature",
			"verify": "go test ./feature",
			"repeat_shaped": false
		}
	]`
	if err := os.WriteFile(jsonArrayFile, []byte(jsonArrayData), 0o644); err != nil {
		t.Fatalf("failed writing jsonArrayFile: %v", err)
	}

	tasks, err := LoadTasks(jsonArrayFile)
	if err != nil {
		t.Fatalf("unexpected error loading json array tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ID != "task-01" || !tasks[0].RepeatShaped {
		t.Errorf("unexpected task 0: %+v", tasks[0])
	}
	if tasks[1].ID != "task-02" || tasks[1].RepeatShaped {
		t.Errorf("unexpected task 1: %+v", tasks[1])
	}

	// 2. Test JSONL format
	jsonlFile := filepath.Join(tmpDir, "tasks.jsonl")
	jsonlData := "# Comment line\n" +
		`{"id":"task-03","repo":"/repo","start_commit":"111","prompt":"P3","verify":"v3","repeat_shaped":true}` + "\n" +
		`{"id":"task-04","repo":"/repo","start_commit":"222","prompt":"P4","verify":"v4","repeat_shaped":false}` + "\n"
	if err := os.WriteFile(jsonlFile, []byte(jsonlData), 0o644); err != nil {
		t.Fatalf("failed writing jsonlFile: %v", err)
	}

	tasksJSONL, err := LoadTasks(jsonlFile)
	if err != nil {
		t.Fatalf("unexpected error loading jsonl tasks: %v", err)
	}
	if len(tasksJSONL) != 2 {
		t.Fatalf("expected 2 tasks from JSONL, got %d", len(tasksJSONL))
	}
	if tasksJSONL[0].ID != "task-03" || tasksJSONL[1].ID != "task-04" {
		t.Errorf("unexpected tasks from JSONL: %+v", tasksJSONL)
	}

	// 3. Test empty file error
	emptyFile := filepath.Join(tmpDir, "empty.json")
	if err := os.WriteFile(emptyFile, []byte(""), 0o644); err != nil {
		t.Fatalf("failed writing emptyFile: %v", err)
	}
	if _, err := LoadTasks(emptyFile); err == nil {
		t.Error("expected error for empty file, got nil")
	}
}

func TestFormatRecipePrompt(t *testing.T) {
	t.Parallel()

	recipe := "1. Read interface definition\n2. Add implementation\n3. Run tests"
	prompt := "Implement pure-Go parser without CGO"

	got := FormatRecipePrompt(recipe, prompt)
	expected := "<procedure>\n" + recipe + "\n</procedure>\n\n" + prompt
	if got != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, got)
	}
}

func TestParseTraceUsage(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tracePath := filepath.Join(tmpDir, "sample.trace.jsonl")

	traceData := `{"t":"task_start","prompt":"fix issue"}
{"t":"tool_call","name":"read_file","args":"{\"path\":\"foo.go\"}"}
{"t":"tool_result","name":"read_file","bytes":100}
{"t":"task_end","outcome":"verified","usage":{"prompt":1420,"completion":380},"wall_ms":5400}
`
	if err := os.WriteFile(tracePath, []byte(traceData), 0o644); err != nil {
		t.Fatalf("failed writing tracePath: %v", err)
	}

	pTokens, cTokens, wallMS, err := ParseTraceUsage(tracePath)
	if err != nil {
		t.Fatalf("unexpected error parsing trace usage: %v", err)
	}
	if pTokens != 1420 {
		t.Errorf("expected 1420 prompt tokens, got %d", pTokens)
	}
	if cTokens != 380 {
		t.Errorf("expected 380 completion tokens, got %d", cTokens)
	}
	if wallMS != 5400 {
		t.Errorf("expected 5400 wall ms, got %d", wallMS)
	}
}

func TestComputeSummaryAndGate(t *testing.T) {
	t.Parallel()

	results := []Result{
		// Arm A (Floor): Repeat: 1 pass, 1 fail (50%). Novel: 0 pass, 1 fail.
		{TaskID: "t1", Arm: ArmA, RepeatShaped: true, VerifyExitCode: 0},
		{TaskID: "t2", Arm: ArmA, RepeatShaped: true, VerifyExitCode: 1},
		{TaskID: "t3", Arm: ArmA, RepeatShaped: false, VerifyExitCode: 1},

		// Arm B (Recipe): Repeat: 2 pass, 0 fail (100%). Novel: 0 pass, 1 fail.
		// Delta on repeat: 100% - 50% = +50.0 pp >= 25.0 pp -> GATE PASSES
		{TaskID: "t1", Arm: ArmB, RepeatShaped: true, VerifyExitCode: 0},
		{TaskID: "t2", Arm: ArmB, RepeatShaped: true, VerifyExitCode: 0},
		{TaskID: "t3", Arm: ArmB, RepeatShaped: false, VerifyExitCode: 1},

		// Arm C (Ceiling): Repeat: 2 pass, 0 fail (100%). Novel: 1 pass, 0 fail (100%).
		{TaskID: "t1", Arm: ArmC, RepeatShaped: true, VerifyExitCode: 0},
		{TaskID: "t2", Arm: ArmC, RepeatShaped: true, VerifyExitCode: 0},
		{TaskID: "t3", Arm: ArmC, RepeatShaped: false, VerifyExitCode: 0},
	}

	summary := ComputeSummary(results)
	if summary.TotalRuns != 9 {
		t.Errorf("expected 9 total runs, got %d", summary.TotalRuns)
	}
	if summary.RepeatShapedPassRates[ArmA] != 50.0 {
		t.Errorf("expected Arm A repeat pass rate 50%%, got %.1f%%", summary.RepeatShapedPassRates[ArmA])
	}
	if summary.RepeatShapedPassRates[ArmB] != 100.0 {
		t.Errorf("expected Arm B repeat pass rate 100%%, got %.1f%%", summary.RepeatShapedPassRates[ArmB])
	}
	if summary.GateDeltaPercentagePts != 50.0 {
		t.Errorf("expected gate delta 50.0 pp, got %.1f", summary.GateDeltaPercentagePts)
	}
	if !summary.GatePassed {
		t.Errorf("expected GatePassed = true for 50pp delta")
	}

	// Test gate failure when delta is below 25 pp
	failingResults := []Result{
		{TaskID: "t1", Arm: ArmA, RepeatShaped: true, VerifyExitCode: 0},
		{TaskID: "t2", Arm: ArmA, RepeatShaped: true, VerifyExitCode: 0}, // 100%
		{TaskID: "t1", Arm: ArmB, RepeatShaped: true, VerifyExitCode: 0},
		{TaskID: "t2", Arm: ArmB, RepeatShaped: true, VerifyExitCode: 0}, // 100% -> delta 0
	}
	failSummary := ComputeSummary(failingResults)
	if failSummary.GatePassed {
		t.Errorf("expected GatePassed = false for 0pp delta")
	}
}

func TestRunnerIdempotency(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	resultsFile := filepath.Join(tmpDir, "results.jsonl")

	existingResult := Result{
		TaskID:         "task-1",
		Arm:            ArmA,
		VerifyExitCode: 0,
		Timestamp:      time.Now(),
	}
	resData, _ := json.Marshal(existingResult)
	if err := os.WriteFile(resultsFile, append(resData, '\n'), 0o644); err != nil {
		t.Fatalf("failed writing results file: %v", err)
	}

	cfg := Config{
		ResultsFile: resultsFile,
		Arms:        []string{ArmA, ArmB},
	}
	runner, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("NewRunner error: %v", err)
	}

	if !runner.completed["task-1:A"] {
		t.Error("expected task-1:A to be marked completed")
	}
	if runner.completed["task-1:B"] {
		t.Error("expected task-1:B to NOT be marked completed")
	}
}

func TestWorktreeLifecycle(t *testing.T) {
	t.Parallel()

	// Set up a mock git repo
	repoDir := t.TempDir()
	execCommand(t, repoDir, "git", "init")
	execCommand(t, repoDir, "git", "config", "user.name", "TestUser")
	execCommand(t, repoDir, "git", "config", "user.email", "test@example.com")

	filePath := filepath.Join(repoDir, "file.txt")
	_ = os.WriteFile(filePath, []byte("initial commit"), 0o644)
	execCommand(t, repoDir, "git", "add", "file.txt")
	execCommand(t, repoDir, "git", "commit", "-m", "initial")

	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse failed: %v", err)
	}
	commitSHA := strings.TrimSpace(string(out))

	runner := &Runner{}
	wtPath, cleanup, err := runner.createWorktree(repoDir, commitSHA, "task-test", ArmA)
	if err != nil {
		t.Fatalf("createWorktree failed: %v", err)
	}

	// Verify file exists in isolated worktree
	wtFile := filepath.Join(wtPath, "file.txt")
	data, err := os.ReadFile(wtFile)
	if err != nil || string(data) != "initial commit" {
		t.Fatalf("worktree file content mismatch: %v (%s)", err, string(data))
	}

	// Run cleanup and assert worktree directory is removed
	cleanup()
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("expected worktree directory to be removed after cleanup")
	}
}

func execCommand(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("command %s %v failed in %s: %v\nOutput: %s", name, args, dir, err, string(out))
	}
}
