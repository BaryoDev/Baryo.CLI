// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package doctor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// CheckResult holds the outcome of a single diagnostic check.
type CheckResult struct {
	Name    string
	Passed  bool
	Message string // guidance shown on failure
}

// isTCP returns true if the socket path points to a TCP endpoint.
func isTCP(socketPath string) bool {
	if strings.HasPrefix(socketPath, "tcp://") {
		return true
	}
	if _, _, err := net.SplitHostPort(socketPath); err == nil {
		return true
	}
	return false
}

// RunChecks performs the startup diagnostic sequence, stopping at the first failure.
// socketPath is the resolved inference socket path.
func RunChecks(socketPath string) []CheckResult {
	if isTCP(socketPath) {
		return runTCPChecks(socketPath)
	}
	return runLocalChecks(socketPath)
}

// runTCPChecks validates a TCP endpoint (remote Ollama / Model Runner).
func runTCPChecks(socketPath string) []CheckResult {
	var results []CheckResult

	addr := strings.TrimPrefix(socketPath, "tcp://")
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		results = append(results, CheckResult{
			Name:   "Endpoint reachable",
			Passed: false,
			Message: fmt.Sprintf(`Cannot connect to %s

  • Check that the remote server is running
  • Verify your SSH tunnel or network connection
  • Try: curl http://%s/v1/models`, addr, addr),
		})
		return results
	}
	conn.Close()
	results = append(results, CheckResult{
		Name:    "Endpoint reachable",
		Passed:  true,
		Message: addr,
	})

	return results
}

// runLocalChecks validates the local Docker Model Runner setup.
func runLocalChecks(socketPath string) []CheckResult {
	var results []CheckResult

	// 1. Is Docker installed?
	if _, err := exec.LookPath("docker"); err != nil {
		results = append(results, CheckResult{
			Name:   "Docker installed",
			Passed: false,
			Message: `Baryo needs Docker Desktop to run AI models locally.

  1. Download Docker Desktop from https://www.llm.com/products/docker-desktop
  2. Install and launch it
  3. Run baryo again`,
		})
		return results
	}
	results = append(results, CheckResult{Name: "Docker installed", Passed: true})

	// 2. Is Docker running?
	cmd := exec.Command("docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		results = append(results, CheckResult{
			Name:   "Docker running",
			Passed: false,
			Message: `Docker Desktop is installed but not started.

  • Open Docker Desktop from your Applications folder
  • Wait for the whale icon to stop animating
  • Run baryo again`,
		})
		return results
	}
	results = append(results, CheckResult{Name: "Docker running", Passed: true})

	// 3. Is Model Runner enabled? (inference socket exists)
	if _, err := os.Stat(socketPath); err != nil {
		results = append(results, CheckResult{
			Name:   "Model Runner enabled",
			Passed: false,
			Message: fmt.Sprintf(`Docker Desktop is running, but the Model Runner feature is off.

  1. Open Docker Desktop → Settings → Features in development
  2. Enable "Docker Model Runner"
  3. Click "Apply & restart"
  4. Run baryo again

  Expected socket: %s
  Learn more: https://docs.llm.com/desktop/features/model-runner/`, socketPath),
		})
		return results
	}
	results = append(results, CheckResult{Name: "Model Runner enabled", Passed: true})

	// 4. Are any models pulled?
	models, err := llm.ListModels()
	if err != nil {
		results = append(results, CheckResult{
			Name:   "Models available",
			Passed: false,
			Message: fmt.Sprintf(`Could not list models: %v

  Try running: docker model list`, err),
		})
		return results
	}
	if len(models) == 0 {
		results = append(results, CheckResult{
			Name:   "Models available",
			Passed: false,
			Message: `Docker Model Runner is ready, but no models are installed yet.

  Pull a model to get started:

    docker model pull ai/mistral
    docker model pull ai/llama3.2

  Then run baryo again.`,
		})
		return results
	}
	results = append(results, CheckResult{
		Name:   "Models available",
		Passed: true,
		Message: fmt.Sprintf("%d model(s) found", len(models)),
	})

	return results
}

// FormatResults renders check results as a human-readable string with color markers.
func FormatResults(results []CheckResult) string {
	var b strings.Builder

	for _, r := range results {
		if r.Passed {
			b.WriteString(fmt.Sprintf("  \033[32m✓\033[0m %s", r.Name))
			if r.Message != "" {
				b.WriteString(fmt.Sprintf(" — %s", r.Message))
			}
			b.WriteString("\n")
		} else {
			b.WriteString(fmt.Sprintf("  \033[31m✗\033[0m %s\n\n", r.Name))
			b.WriteString(r.Message)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// AllPassed returns true if every check in the results passed.
func AllPassed(results []CheckResult) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}
