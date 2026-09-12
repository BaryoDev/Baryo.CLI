// SPDX-License-Identifier: MIT

// Command baryo-runner executes the Phase 1 three-arm benchmark suite.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/bench"
)

func main() {
	var (
		tasksPath   = flag.String("tasks", "tasks.json", "path to tasks file (.json or .jsonl)")
		baryoBin    = flag.String("baryo", "baryo", "path to baryo binary")
		localModel  = flag.String("local-model", "qwen2.5-coder:7b", "local model name for Arms A & B")
		cloudModel  = flag.String("cloud-model", "claude-3-5-sonnet-20241022", "cloud model name for Arm C")
		recipesDir  = flag.String("recipes", "recipes", "directory containing <task_id>.md recipes")
		tracesDir   = flag.String("traces", "traces", "directory to write per-run trace files")
		resultsPath = flag.String("results", "results.jsonl", "path to results file")
		timeoutStr  = flag.String("timeout", "5m", "timeout duration per task execution")
		armsStr     = flag.String("arms", "A,B,C", "comma-separated list of arms to run (e.g. A,B,C)")
		summaryOnly = flag.Bool("summary", false, "display summary and Phase 1 gate check for existing results")
	)
	flag.Parse()

	if *summaryOnly {
		runSummary(*resultsPath)
		return
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid timeout duration: %v\n", err)
		os.Exit(2)
	}

	var arms []string
	for _, a := range strings.Split(*armsStr, ",") {
		trimmed := strings.TrimSpace(strings.ToUpper(a))
		if trimmed != "" {
			arms = append(arms, trimmed)
		}
	}

	tasks, err := bench.LoadTasks(*tasksPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading tasks from %s: %v\n", *tasksPath, err)
		os.Exit(2)
	}

	cfg := bench.Config{
		BaryoBinary: *baryoBin,
		LocalModel:  *localModel,
		CloudModel:  *cloudModel,
		RecipesDir:  *recipesDir,
		TracesDir:   *tracesDir,
		ResultsFile: *resultsPath,
		Timeout:     timeout,
		Arms:        arms,
	}

	runner, err := bench.NewRunner(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing runner: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	fmt.Printf("=== Baryo Phase 1 Benchmark Runner ===\n")
	fmt.Printf("Tasks: %d | Arms: %s | Local Model: %s | Cloud Model: %s\n\n", len(tasks), strings.Join(arms, ", "), *localModel, *cloudModel)

	results, err := runner.Run(ctx, tasks)
	if err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "Benchmark run encountered an error: %v\n", err)
	}

	fmt.Println("\n=== Phase 1 Benchmark Summary ===")
	runSummary(*resultsPath)

	_ = results
}

func runSummary(resultsPath string) {
	data, err := os.ReadFile(resultsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not read results file %s: %v\n", resultsPath, err)
		return
	}

	var results []bench.Result
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var r bench.Result
		if err := json.Unmarshal([]byte(trimmed), &r); err == nil {
			results = append(results, r)
		}
	}

	summary := bench.ComputeSummary(results)
	fmt.Printf("Total evaluated runs: %d\n", summary.TotalRuns)
	for _, arm := range []string{bench.ArmA, bench.ArmB, bench.ArmC} {
		fmt.Printf("Arm %s Pass Rate: %.1f%% (Repeat-shaped: %.1f%%)\n",
			arm,
			summary.ArmPassRates[arm],
			summary.RepeatShapedPassRates[arm],
		)
	}
	fmt.Printf("Repeat-shaped Delta (Arm B - Arm A): %+.1f percentage points (Target: >= +25.0 pp)\n", summary.GateDeltaPercentagePts)
	if summary.GatePassed {
		fmt.Printf(">> Phase 1 Gate: PASSED (Hypothesis validated) <<\n")
	} else {
		fmt.Printf(">> Phase 1 Gate: NOT YET MET <<\n")
	}
}
