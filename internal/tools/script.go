// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/procutil"
	"github.com/arnelirobles/baryo-cli/internal/sandbox"
)

const scriptTimeout = 120 * time.Second
const maxScriptOutput = 200 * 1024 // 200 KB

// sandboxInstance holds the optional sandbox for code execution.
// Set via EnableSandbox() at startup when --sandbox is active.
var sandboxInstance *sandbox.Sandbox

// EnableSandbox activates sandboxed code execution via Docker containers.
func EnableSandbox() {
	s := sandbox.New()
	if s.Available {
		sandboxInstance = s
	}
}

func init() {
	Register("run_script", Tool{
		Destructive: true,
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "run_script",
				Description: "Run an existing script file and return its output. Use for executing skill scripts.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the script file (e.g. skills/pdf/scripts/extract_form_field_info.py)",
						},
						"args": map[string]interface{}{
							"type":        "string",
							"description": "Command-line arguments to pass to the script",
						},
					},
					"required": []string{"path"},
				},
			},
		},
		Execute: executeRunScript,
	})

	Register("run_code", Tool{
		Destructive: true,
		Def: Definition{
			Type: "function",
			Function: FunctionDef{
				Name:        "run_code",
				Description: "Execute inline code (Python, shell, etc.) and return the output. Use this when you need to write and run code on the fly, such as generating files using skill libraries. The code runs from the specified working directory so it can import skill modules.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"code": map[string]interface{}{
							"type":        "string",
							"description": "The code to execute",
						},
						"language": map[string]interface{}{
							"type":        "string",
							"description": "Programming language: python, shell, node",
							"enum":        []string{"python", "shell", "node"},
						},
						"working_dir": map[string]interface{}{
							"type":        "string",
							"description": "Working directory to run from (e.g. skills/slack-gif-creator so imports like 'from core.gif_builder import GIFBuilder' work)",
						},
					},
					"required": []string{"code", "language"},
				},
			},
		},
		Execute: executeRunCode,
	})
}

type runScriptArgs struct {
	Path string `json:"path"`
	Args string `json:"args"`
}

type runCodeArgs struct {
	Code       string `json:"code"`
	Language   string `json:"language"`
	WorkingDir string `json:"working_dir"`
}

func executeRunScript(ctx context.Context, argsJSON string) Result {
	var args runScriptArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	if args.Path == "" {
		return Result{Content: "path is required", IsError: true}
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(args.Path)
	if err != nil {
		return Result{Content: fmt.Sprintf("invalid path: %v", err), IsError: true}
	}

	// Safety: restrict execution to skill directories only
	if !isInsideSkillDir(absPath) {
		return Result{Content: fmt.Sprintf("path %q is outside allowed skill directories", args.Path), IsError: true}
	}

	// Safety: verify the script file exists
	info, err := os.Stat(absPath)
	if err != nil {
		return Result{Content: fmt.Sprintf("script not found: %s", args.Path), IsError: true}
	}
	if info.IsDir() {
		return Result{Content: "path is a directory, not a script", IsError: true}
	}

	// Determine the interpreter based on extension
	ext := strings.ToLower(filepath.Ext(absPath))
	var cmdArgs []string
	switch ext {
	case ".py":
		// Check for venv in the script's parent directories
		venvPython := findVenvPython(filepath.Dir(absPath))
		if venvPython != "" {
			cmdArgs = []string{venvPython, absPath}
		} else {
			cmdArgs = []string{"python3", absPath}
		}
	case ".sh":
		cmdArgs = []string{"sh", absPath}
	case ".js":
		cmdArgs = []string{"node", absPath}
	case ".ts":
		cmdArgs = []string{"npx", "ts-node", absPath}
	case ".rb":
		cmdArgs = []string{"ruby", absPath}
	default:
		cmdArgs = []string{absPath}
	}

	// Append user arguments
	if args.Args != "" {
		cmdArgs = append(cmdArgs, strings.Fields(args.Args)...)
	}

	return runCommand(ctx, cmdArgs, filepath.Dir(absPath))
}

func executeRunCode(ctx context.Context, argsJSON string) Result {
	var args runCodeArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}
	}

	if args.Code == "" {
		return Result{Content: "code is required", IsError: true}
	}

	// Route through sandbox if enabled
	if sandboxInstance != nil && sandboxInstance.Available {
		workDir, _ := os.Getwd()
		if args.WorkingDir != "" {
			if absDir, err := filepath.Abs(args.WorkingDir); err == nil {
				workDir = absDir
			}
		}
		output, err := sandboxInstance.Execute(ctx, args.Language, args.Code, workDir)
		if err != nil {
			return Result{Content: fmt.Sprintf("[sandbox] %v\n%s", err, output), IsError: true}
		}
		if output == "" {
			output = "(no output)"
		}
		return Result{Content: "[sandbox] " + output}
	}

	// Determine working directory
	workDir, _ := os.Getwd()
	if args.WorkingDir != "" {
		absDir, err := filepath.Abs(args.WorkingDir)
		if err == nil {
			if info, err := os.Stat(absDir); err == nil && info.IsDir() {
				workDir = absDir
			}
		}
	}

	// Write code to a temp file
	var ext string
	var interpreter []string
	switch args.Language {
	case "python":
		ext = ".py"
		// Use skill's venv Python if available
		venvPython := filepath.Join(workDir, ".venv", "bin", "python3")
		if _, err := os.Stat(venvPython); err == nil {
			interpreter = []string{venvPython}
		} else {
			interpreter = []string{"python3"}
		}
	case "shell":
		ext = ".sh"
		interpreter = []string{"sh"}
	case "node":
		ext = ".js"
		interpreter = []string{"node"}
	default:
		ext = ".py"
		interpreter = []string{"python3"}
	}

	tmpFile, err := os.CreateTemp("", "baryo-code-*"+ext)
	if err != nil {
		return Result{Content: fmt.Sprintf("failed to create temp file: %v", err), IsError: true}
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(args.Code); err != nil {
		tmpFile.Close()
		return Result{Content: fmt.Sprintf("failed to write code: %v", err), IsError: true}
	}
	tmpFile.Close()

	cmdArgs := append(interpreter, tmpFile.Name())
	return runCommand(ctx, cmdArgs, workDir)
}

// isInsideSkillDir checks that the given absolute path is inside an allowed
// skill directory. This prevents path traversal via run_script.
func isInsideSkillDir(absPath string) bool {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()

	allowed := []string{
		filepath.Join(cwd, "skills"),
		filepath.Join(cwd, ".baryo", "skills"),
	}
	if home != "" {
		allowed = append(allowed, filepath.Join(home, ".baryo", "skills"))
	}

	for _, dir := range allowed {
		// Ensure trailing separator so "/skills-other" doesn't match "/skills"
		if strings.HasPrefix(absPath, dir+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// findVenvPython looks for a .venv/bin/python3 in the given directory or its parents.
func findVenvPython(dir string) string {
	for i := 0; i < 4; i++ { // check up to 4 levels up
		venv := filepath.Join(dir, ".venv", "bin", "python3")
		if _, err := os.Stat(venv); err == nil {
			return venv
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// runCommand executes a command with timeout and returns the result.
// After successful execution, it detects newly created files so the model
// can report file paths to the user even when the code has no stdout.
func runCommand(ctx context.Context, cmdArgs []string, workDir string) Result {
	startTime := time.Now()

	execCtx, cancel := context.WithTimeout(ctx, scriptTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = workDir
	// Kill the whole process group on timeout/cancel so grandchildren
	// (e.g. backgrounded shell commands) don't outlive the tool call.
	procutil.SetProcessGroup(cmd)
	// Strip any inherited virtual environment from the user's shell so one
	// skill's venv doesn't interfere with another skill's execution.
	// Each skill uses its own venv (via absolute path) or the system python.
	env := stripVenv(os.Environ())
	// Include working directory in PYTHONPATH so imports resolve for code
	// run from temp files (whose directory is /tmp, not the working dir).
	cmd.Env = append(env, "PYTHONPATH="+workDir)
	var buf procutil.CappedBuffer
	buf.Max = maxScriptOutput
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()

	output := buf.String()
	if buf.Truncated() {
		output += "\n... (output truncated)"
	}

	if err != nil {
		if output == "" {
			return Result{Content: fmt.Sprintf("failed: %v", err), IsError: true}
		}
		return Result{Content: fmt.Sprintf("failed: %v\n%s", err, output), IsError: true}
	}

	// Detect files created/modified during execution so the model can
	// report them even when the code produces no stdout.
	newFiles := detectNewFiles(startTime, workDir)
	if output == "" {
		if len(newFiles) > 0 {
			output = "Files created:\n" + strings.Join(newFiles, "\n")
		} else {
			output = "(completed with no output)"
		}
	} else if len(newFiles) > 0 {
		output += "\n\nFiles created:\n" + strings.Join(newFiles, "\n")
	}

	return Result{Content: output, IsError: false}
}

// stripVenv removes VIRTUAL_ENV from the environment and strips its bin
// directory from PATH. This prevents an activated venv in the user's shell
// from leaking into skill subprocesses.
func stripVenv(environ []string) []string {
	var venvDir string
	for _, e := range environ {
		if strings.HasPrefix(e, "VIRTUAL_ENV=") {
			venvDir = e[len("VIRTUAL_ENV="):]
			break
		}
	}
	if venvDir == "" {
		return environ
	}

	venvBin := filepath.Join(venvDir, "bin")
	result := make([]string, 0, len(environ))
	for _, e := range environ {
		if strings.HasPrefix(e, "VIRTUAL_ENV=") {
			continue
		}
		if strings.HasPrefix(e, "PATH=") {
			pathVal := e[len("PATH="):]
			parts := strings.Split(pathVal, string(os.PathListSeparator))
			var cleaned []string
			for _, p := range parts {
				if p != venvBin {
					cleaned = append(cleaned, p)
				}
			}
			result = append(result, "PATH="+strings.Join(cleaned, string(os.PathListSeparator)))
			continue
		}
		result = append(result, e)
	}
	return result
}

// detectNewFiles finds files created or modified after startTime in the given
// directories. It checks workDir plus the user's cwd (if different).
func detectNewFiles(startTime time.Time, workDir string) []string {
	cwd, _ := os.Getwd()
	outDir := filepath.Join(cwd, "output_files")

	dirs := []string{workDir}
	if cwd != workDir {
		dirs = append(dirs, cwd)
	}
	if outDir != workDir && outDir != cwd {
		dirs = append(dirs, outDir)
	}

	seen := make(map[string]bool)
	var files []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(startTime) {
				continue
			}
			absPath := filepath.Join(dir, e.Name())
			if seen[absPath] {
				continue
			}
			seen[absPath] = true
			files = append(files, absPath)
		}
	}
	return files
}
