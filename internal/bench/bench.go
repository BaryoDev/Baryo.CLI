// SPDX-License-Identifier: MIT

// Package bench implements the three-arm Phase 1 benchmark runner for Baryo.
//
// It evaluates tasks from the Phase 1 benchmark set across three experimental arms:
//   - Arm A: Local model alone, no recipe (the floor)
//   - Arm B: Local model with hand-distilled recipe injected (the hypothesis)
//   - Arm C: Cloud model alone, no recipe (the ceiling)
//
// For each task and arm, the runner isolates execution in a freshly checked-out git
// worktree at the parent commit, enforces timeouts, enables project trust where .baryo
// exists, collects execution traces, and runs the machine-checkable verify command.
package bench

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Standard arms for Phase 1.
const (
	ArmA = "A" // local model, no recipe (floor)
	ArmB = "B" // local model with recipe (test)
	ArmC = "C" // cloud model, no recipe (ceiling)
)

// Phase 1 Gate status values.
const (
	GateStatusPassed           = "PASSED"
	GateStatusNotMet           = "NOT_MET"
	GateStatusInsufficientData = "INSUFFICIENT_DATA"
)

// Task represents a single benchmark evaluation task.
type Task struct {
	ID            string `json:"id"`
	Repo          string `json:"repo"`
	StartCommit   string `json:"start_commit"`
	Prompt        string `json:"prompt"`
	VerifyCommand string `json:"verify"`
	ReferenceSHA  string `json:"reference,omitempty"`
	RepeatShaped  bool   `json:"repeat_shaped"`
	Recipe        string `json:"recipe,omitempty"`
}

// Result records the outcome of a single arm running on a task.
type Result struct {
	TaskID           string    `json:"task_id"`
	Arm              string    `json:"arm"`
	Model            string    `json:"model"`
	StartCommit      string    `json:"start_commit"`
	RepeatShaped     bool      `json:"repeat_shaped"`
	InfraFailure     bool      `json:"infra_failure,omitempty"`
	RunExitCode      int       `json:"run_exit_code"`
	RunError         string    `json:"run_error,omitempty"`
	VerifyExitCode   int       `json:"verify_exit_code"`
	VerifyOutput     string    `json:"verify_output,omitempty"`
	WallClockMS      int64     `json:"wall_clock_ms"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TraceFile        string    `json:"trace_file"`
	Timestamp        time.Time `json:"timestamp"`
}

// Config configures the three-arm runner.
type Config struct {
	BaryoBinary   string        // Path to baryo executable (default: "baryo")
	LocalModel    string        // Model identifier for Arms A and B
	CloudModel    string        // Model identifier for Arm C
	RecipesDir    string        // Optional directory with <task_id>.md recipes
	TracesDir     string        // Directory to store per-run trace files
	ResultsFile   string        // Path to results file (JSONL format)
	Timeout       time.Duration // Timeout per baryo run (default: 5m)
	VerifyTimeout time.Duration // Timeout per verify command execution (default: 2m)
	Arms          []string      // List of arms to execute (default: ["A", "B", "C"])
	Stdout        io.Writer
	Stderr        io.Writer
}

// Runner orchestrates the evaluation of tasks across arms.
type Runner struct {
	cfg       Config
	completed map[string]bool // key: "taskID:arm"
}

// NewRunner creates a new benchmark runner with the provided configuration.
func NewRunner(cfg Config) (*Runner, error) {
	if cfg.BaryoBinary == "" {
		cfg.BaryoBinary = "baryo"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	if cfg.VerifyTimeout <= 0 {
		cfg.VerifyTimeout = 2 * time.Minute
	}
	if len(cfg.Arms) == 0 {
		cfg.Arms = []string{ArmA, ArmB, ArmC}
	}
	if cfg.TracesDir == "" {
		cfg.TracesDir = "traces"
	}
	if absTraces, err := filepath.Abs(cfg.TracesDir); err == nil {
		cfg.TracesDir = absTraces
	}
	if cfg.ResultsFile == "" {
		cfg.ResultsFile = "results.jsonl"
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}

	r := &Runner{
		cfg:       cfg,
		completed: make(map[string]bool),
	}

	// Load existing results to ensure idempotency.
	if err := r.loadExistingResults(); err != nil {
		return nil, fmt.Errorf("loading existing results: %w", err)
	}

	return r, nil
}

// loadExistingResults reads existing results from ResultsFile to make runs idempotent.
// Results with InfraFailure are excluded so they can be retried.
func (r *Runner) loadExistingResults() error {
	f, err := os.Open(r.cfg.ResultsFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var res Result
		if err := json.Unmarshal([]byte(line), &res); err != nil {
			continue
		}
		if res.InfraFailure {
			continue
		}
		key := fmt.Sprintf("%s:%s", res.TaskID, res.Arm)
		r.completed[key] = true
	}
	return scanner.Err()
}

// LoadTasks reads tasks from a JSON or JSONL file.
func LoadTasks(path string) ([]Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading tasks file: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, errors.New("no tasks found in file")
	}

	var tasks []Task
	// First try JSON array format
	if err := json.Unmarshal(data, &tasks); err == nil {
		if len(tasks) == 0 {
			return nil, errors.New("no tasks found in file")
		}
		return tasks, nil
	}

	// Fallback to JSONL format
	scanner := bufio.NewScanner(strings.NewReader(trimmed))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var t Task
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			return nil, fmt.Errorf("parsing task JSON line %q: %w", line, err)
		}
		tasks = append(tasks, t)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, errors.New("no tasks found in file")
	}
	return tasks, nil
}

// Run executes all tasks across configured arms.
func (r *Runner) Run(ctx context.Context, tasks []Task) ([]Result, error) {
	if err := os.MkdirAll(r.cfg.TracesDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating traces directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(r.cfg.ResultsFile), 0o755); err != nil && filepath.Dir(r.cfg.ResultsFile) != "." {
		return nil, fmt.Errorf("creating results directory: %w", err)
	}

	var allResults []Result
	for _, task := range tasks {
		for _, arm := range r.cfg.Arms {
			if err := ctx.Err(); err != nil {
				return allResults, err
			}

			key := fmt.Sprintf("%s:%s", task.ID, arm)
			if r.completed[key] {
				fmt.Fprintf(r.cfg.Stdout, "[-] Skipping %s (already completed)\n", key)
				continue
			}

			fmt.Fprintf(r.cfg.Stdout, "[*] Running task %s | Arm %s...\n", task.ID, arm)
			res, err := r.runTaskArm(ctx, task, arm)
			if err != nil {
				fmt.Fprintf(r.cfg.Stderr, "Error running task %s arm %s: %v\n", task.ID, arm, err)
			}

			if err := r.recordResult(res); err != nil {
				fmt.Fprintf(r.cfg.Stderr, "Error recording result for %s: %v\n", key, err)
			}
			if !res.InfraFailure {
				r.completed[key] = true
			}
			allResults = append(allResults, res)
		}
	}

	return allResults, nil
}

// runTaskArm runs a single arm on a single task.
func (r *Runner) runTaskArm(ctx context.Context, task Task, arm string) (Result, error) {
	start := time.Now()
	res := Result{
		TaskID:         task.ID,
		Arm:            arm,
		StartCommit:    task.StartCommit,
		RepeatShaped:   task.RepeatShaped,
		VerifyExitCode: -1, // default unrun/failed verify
		Timestamp:      time.Now().UTC(),
	}

	// 1. Model resolution per arm
	switch arm {
	case ArmA, ArmB:
		res.Model = r.cfg.LocalModel
	case ArmC:
		res.Model = r.cfg.CloudModel
	default:
		res.InfraFailure = true
		return res, fmt.Errorf("unknown arm %q", arm)
	}

	// 2. Validate task has machine-checkable verify command
	if strings.TrimSpace(task.VerifyCommand) == "" {
		res.InfraFailure = true
		res.RunError = "task has no verify command"
		res.WallClockMS = time.Since(start).Milliseconds()
		return res, errors.New("task has no verify command")
	}

	// 3. Prepare isolated git worktree
	worktreePath, cleanup, err := r.createWorktree(task.Repo, task.StartCommit, task.ID, arm)
	if err != nil {
		res.InfraFailure = true
		res.RunError = fmt.Sprintf("worktree creation failed: %v", err)
		res.RunExitCode = 1
		res.WallClockMS = time.Since(start).Milliseconds()
		return res, err
	}
	defer cleanup()

	// 4. Prepare prompt and inject recipe for Arm B
	prompt := task.Prompt
	if arm == ArmB {
		recipe, err := r.resolveRecipe(task)
		if err != nil {
			res.InfraFailure = true
			res.RunError = fmt.Sprintf("recipe resolution failed: %v", err)
			res.RunExitCode = 1
			res.WallClockMS = time.Since(start).Milliseconds()
			return res, err
		}
		prompt = FormatRecipePrompt(recipe, task.Prompt)
	}

	// 5. Trace file configuration: ensure absolute path and purge stale trace file
	traceFileName := fmt.Sprintf("%s_arm_%s.jsonl", task.ID, arm)
	traceFile := filepath.Join(r.cfg.TracesDir, traceFileName)
	if abs, err := filepath.Abs(traceFile); err == nil {
		traceFile = abs
	}
	_ = os.Remove(traceFile)
	res.TraceFile = traceFile

	// 6. Execute Baryo in headless print mode
	runExitCode, runErr := r.executeBaryo(ctx, worktreePath, prompt, res.Model, traceFile)
	res.RunExitCode = runExitCode
	if runErr != nil {
		res.RunError = runErr.Error()
	}

	// Parse token usage and wall clock from trace file if available
	if promptTokens, compTokens, wallMS, err := ParseTraceUsage(traceFile); err == nil {
		res.PromptTokens = promptTokens
		res.CompletionTokens = compTokens
		if wallMS > 0 {
			res.WallClockMS = wallMS
		}
	}
	if res.WallClockMS == 0 {
		res.WallClockMS = time.Since(start).Milliseconds()
	}

	// 7. Execute machine verify command inside worktree
	vCode, vOut, vErr := r.runVerify(ctx, worktreePath, task.VerifyCommand)
	res.VerifyExitCode = vCode
	res.VerifyOutput = vOut
	if vErr != nil && res.RunError == "" {
		res.RunError = fmt.Sprintf("verify error: %v", vErr)
	}

	return res, nil
}

// createWorktree sets up an isolated git worktree at the start commit.
func (r *Runner) createWorktree(repo, commit, taskID, arm string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("baryo_bench_%s_%s_*", taskID, arm))
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir for worktree: %w", err)
	}

	// Use --detach to checkout without creating an unnecessary local branch
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "--detach", tmpDir, commit)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("git worktree add failed (%s): %w", string(output), err)
	}

	cleanup := func() {
		rmCmd := exec.Command("git", "-C", repo, "worktree", "remove", "--force", tmpDir)
		_ = rmCmd.Run()
		_ = os.RemoveAll(tmpDir)
	}

	return tmpDir, cleanup, nil
}

// resolveRecipe retrieves the recipe text for Arm B from the task or recipes directory.
func (r *Runner) resolveRecipe(task Task) (string, error) {
	if task.Recipe != "" {
		return task.Recipe, nil
	}
	if r.cfg.RecipesDir != "" {
		for _, ext := range []string{".md", ".txt"} {
			path := filepath.Join(r.cfg.RecipesDir, task.ID+ext)
			if data, err := os.ReadFile(path); err == nil {
				return string(data), nil
			}
		}
	}
	return "", fmt.Errorf("no recipe found for task %s (checked task.Recipe and %s)", task.ID, r.cfg.RecipesDir)
}

// FormatRecipePrompt wraps a recipe procedure around a task prompt.
func FormatRecipePrompt(recipe, prompt string) string {
	var sb strings.Builder
	sb.WriteString("<procedure>\n")
	sb.WriteString(strings.TrimSpace(recipe))
	sb.WriteString("\n</procedure>\n\n")
	sb.WriteString(strings.TrimSpace(prompt))
	return sb.String()
}

// executeBaryo runs Baryo CLI in headless print mode within the specified worktree.
func (r *Runner) executeBaryo(ctx context.Context, worktree, prompt, model, traceFile string) (int, error) {
	// Set execution deadline
	runCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	args := []string{
		"-p", prompt,
		"--yolo",
		"--trace-file", traceFile,
		"--timeout", r.cfg.Timeout.String(),
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	// Check if repository ships .baryo (enables --trust-project per #16)
	baryoDir := filepath.Join(worktree, ".baryo")
	if fi, err := os.Stat(baryoDir); err == nil && fi.IsDir() {
		args = append(args, "--trust-project")
	}

	cmd := exec.CommandContext(runCtx, r.cfg.BaryoBinary, args...)
	cmd.Dir = worktree
	cmd.WaitDelay = 2 * time.Second

	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return 124, fmt.Errorf("baryo timed out after %v", r.cfg.Timeout)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), fmt.Errorf("baryo failed (exit %d): %s", exitErr.ExitCode(), strings.TrimSpace(string(output)))
		}
		return 1, fmt.Errorf("baryo execution error: %w", err)
	}

	return 0, nil
}

// runVerify executes the machine-checkable verify command inside the worktree with a dedicated timeout.
func (r *Runner) runVerify(ctx context.Context, worktree, verifyCmd string) (int, string, error) {
	verifyTimeout := r.cfg.VerifyTimeout
	if verifyTimeout <= 0 {
		verifyTimeout = 2 * time.Minute
	}
	verifyCtx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	cmd := exec.CommandContext(verifyCtx, "sh", "-c", verifyCmd)
	cmd.Dir = worktree
	cmd.WaitDelay = 2 * time.Second

	output, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(output))
	if len(outStr) > 4096 {
		outStr = outStr[:4096] + "\n... (truncated)"
	}

	if err != nil {
		if errors.Is(verifyCtx.Err(), context.DeadlineExceeded) {
			return 124, outStr, fmt.Errorf("verify command timed out after %v", verifyTimeout)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), outStr, nil
		}
		return 1, outStr, err
	}
	return 0, outStr, nil
}

// ParseTraceUsage inspects a Baryo trace file and extracts token counts and wall clock ms
// from the last task_end record in the file.
func ParseTraceUsage(tracePath string) (promptTokens, completionTokens int, wallMS int64, err error) {
	f, err := os.Open(tracePath)
	if err != nil {
		return 0, 0, 0, err
	}
	defer f.Close()

	var found bool
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, `"task_end"`) {
			continue
		}
		var record struct {
			Type   string         `json:"t"`
			Usage  map[string]int `json:"usage"`
			WallMS int64          `json:"wall_ms"`
		}
		if err := json.Unmarshal([]byte(line), &record); err == nil && record.Type == "task_end" {
			if record.Usage != nil {
				promptTokens = record.Usage["prompt"]
				completionTokens = record.Usage["completion"]
			}
			wallMS = record.WallMS
			found = true
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, 0, err
	}
	if !found {
		return 0, 0, 0, nil
	}
	return promptTokens, completionTokens, wallMS, nil
}

// recordResult appends one result row to the configured ResultsFile in JSONL format.
func (r *Runner) recordResult(res Result) error {
	f, err := os.OpenFile(r.cfg.ResultsFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	line, err := json.Marshal(res)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// Summary evaluates the benchmark results against the Phase 1 Gate.
type Summary struct {
	TotalRuns              int                `json:"total_runs"`
	ArmPassRates           map[string]float64 `json:"arm_pass_rates"`
	RepeatShapedPassRates  map[string]float64 `json:"repeat_shaped_pass_rates"`
	GateDeltaPercentagePts float64            `json:"gate_delta_percentage_pts"`
	GateStatus             string             `json:"gate_status"` // PASSED, NOT_MET, INSUFFICIENT_DATA
	GatePassed             bool               `json:"gate_passed"` // true only if Arm B - Arm A >= 25% on repeat-shaped with valid floor data
}

// ComputeSummary aggregates result metrics and checks the Phase 1 Gate.
// Infrastructure failures (InfraFailure == true) are excluded from pass rates and gate checks.
func ComputeSummary(results []Result) Summary {
	totalPerArm := make(map[string]int)
	passPerArm := make(map[string]int)

	totalRepeatPerArm := make(map[string]int)
	passRepeatPerArm := make(map[string]int)

	var validRuns int
	for _, res := range results {
		if res.InfraFailure {
			continue
		}
		validRuns++
		arm := res.Arm
		totalPerArm[arm]++
		if res.VerifyExitCode == 0 {
			passPerArm[arm]++
		}
		if res.RepeatShaped {
			totalRepeatPerArm[arm]++
			if res.VerifyExitCode == 0 {
				passRepeatPerArm[arm]++
			}
		}
	}

	passRates := make(map[string]float64)
	repeatRates := make(map[string]float64)

	for _, arm := range []string{ArmA, ArmB, ArmC} {
		if totalPerArm[arm] > 0 {
			passRates[arm] = float64(passPerArm[arm]) / float64(totalPerArm[arm]) * 100.0
		}
		if totalRepeatPerArm[arm] > 0 {
			repeatRates[arm] = float64(passRepeatPerArm[arm]) / float64(totalRepeatPerArm[arm]) * 100.0
		}
	}

	// Gate: Arm B must beat Arm A by at least 25 percentage points on repeat-shaped subset.
	// Both Arm A (floor/control) and Arm B (hypothesis) must have at least one evaluated repeat-shaped run.
	var delta float64
	var gatePassed bool
	gateStatus := GateStatusInsufficientData

	if totalRepeatPerArm[ArmA] > 0 && totalRepeatPerArm[ArmB] > 0 {
		delta = repeatRates[ArmB] - repeatRates[ArmA]
		gatePassed = delta >= 25.0
		if gatePassed {
			gateStatus = GateStatusPassed
		} else {
			gateStatus = GateStatusNotMet
		}
	}

	return Summary{
		TotalRuns:              validRuns,
		ArmPassRates:           passRates,
		RepeatShapedPassRates:  repeatRates,
		GateDeltaPercentagePts: delta,
		GateStatus:             gateStatus,
		GatePassed:             gatePassed,
	}
}
