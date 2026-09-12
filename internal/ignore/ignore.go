// SPDX-License-Identifier: MIT

package ignore

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

// builtinPatterns are always active — protect sensitive files without config.
var builtinPatterns = []string{
	".env",
	".env.*",
	"*.pem",
	"*.key",
}

// cached .baryoignore state
var (
	cacheMu     sync.Mutex
	cachedRules []rule
	cachedMtime time.Time
	cachedPath  string
)

// rule is a parsed .baryoignore line.
type rule struct {
	pattern  string
	negate   bool
	dirOnly  bool
	basename bool // true when pattern contains no '/'
}

// IsIgnored returns true if the path should be excluded.
// It checks: builtin patterns → .baryoignore → git check-ignore.
//
// This spawns a git subprocess per call. Use Filter for more than one path.
func IsIgnored(ctx context.Context, absPath string) bool {
	if matchesInProcess(loadRules(), absPath) {
		return true
	}
	return isGitIgnored(ctx, absPath)
}

// Filter returns the subset of absPaths that should be excluded.
//
// Builtin patterns and .baryoignore are applied in process, then git is asked
// about whatever is left in a single subprocess. IsIgnored forks git once per
// path, which on a walk of N files costs N process spawns.
func Filter(ctx context.Context, absPaths []string) map[string]bool {
	ignored := make(map[string]bool, len(absPaths))
	if len(absPaths) == 0 {
		return ignored
	}

	rules := loadRules()
	remaining := make([]string, 0, len(absPaths))
	for _, p := range absPaths {
		if matchesInProcess(rules, p) {
			ignored[p] = true
			continue
		}
		remaining = append(remaining, p)
	}
	if len(remaining) == 0 {
		return ignored
	}

	for p := range gitCheckIgnore(ctx, commonDir(remaining), remaining) {
		ignored[p] = true
	}
	return ignored
}

// matchesInProcess reports whether the builtin patterns or .baryoignore rules
// exclude a path, without touching git.
func matchesInProcess(rules []rule, absPath string) bool {
	name := filepath.Base(absPath)
	for _, pat := range builtinPatterns {
		if ok, _ := doublestar.Match(pat, name); ok {
			return true
		}
	}
	return matchRules(rules, absPath)
}

// commonDir returns the deepest directory containing every path, which git is
// run from so one call can cover the whole batch.
func commonDir(paths []string) string {
	dir := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		for dir != string(filepath.Separator) && dir != "." {
			if p == dir || strings.HasPrefix(p, dir+string(filepath.Separator)) {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return dir
}

// gitCheckIgnore asks git about every path in one subprocess. It is a variable
// so tests can count spawns.
//
// --stdin with -z needs NUL-separated input as well as output: newline-separated
// input is read as a single path name and silently matches nothing.
var gitCheckIgnore = func(ctx context.Context, workDir string, absPaths []string) map[string]bool {
	ignored := make(map[string]bool)
	if len(absPaths) == 0 {
		return ignored
	}

	cmd := exec.CommandContext(ctx, "git", "check-ignore", "--stdin", "-z")
	cmd.Dir = workDir
	cmd.Stdin = strings.NewReader(strings.Join(absPaths, "\x00"))
	out, err := cmd.Output()
	if err != nil {
		// Exit 1 means none were ignored; anything else means git is
		// unavailable or unhappy, and then nothing is excluded on its behalf.
		return ignored
	}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			ignored[p] = true
		}
	}
	return ignored
}

// loadRules returns the cached .baryoignore rules, re-parsing if the file changed.
func loadRules() []rule {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	path := filepath.Join(cwd, ".baryoignore")

	info, err := os.Stat(path)
	if err != nil {
		return nil
	}

	cacheMu.Lock()
	defer cacheMu.Unlock()

	if path == cachedPath && info.ModTime().Equal(cachedMtime) {
		return cachedRules
	}

	rules := parseFile(path)
	cachedRules = rules
	cachedMtime = info.ModTime()
	cachedPath = path
	return rules
}

// parseFile reads a .baryoignore file and returns parsed rules.
func parseFile(path string) []rule {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var rules []rule
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()

		// Strip trailing whitespace.
		line = strings.TrimRight(line, " \t")

		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		r := rule{}

		// Negation prefix.
		if strings.HasPrefix(line, "!") {
			r.negate = true
			line = line[1:]
		}

		// Trailing slash → directory-only.
		if strings.HasSuffix(line, "/") {
			r.dirOnly = true
			line = strings.TrimRight(line, "/")
		}

		// If the pattern has no slash, it matches basename only.
		r.basename = !strings.Contains(line, "/")
		r.pattern = line

		rules = append(rules, r)
	}
	return rules
}

// matchRules evaluates .baryoignore rules against absPath.
// Returns true if the path is ignored (not negated).
func matchRules(rules []rule, absPath string) bool {
	if len(rules) == 0 {
		return false
	}

	cwd, err := os.Getwd()
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(cwd, absPath)
	if err != nil {
		return false
	}
	// Normalize to forward slashes for matching.
	rel = filepath.ToSlash(rel)
	name := filepath.Base(absPath)

	isDir := false
	if info, err := os.Stat(absPath); err == nil {
		isDir = info.IsDir()
	}

	matched := false
	for _, r := range rules {
		if r.dirOnly && !isDir {
			continue
		}

		var ok bool
		if r.basename {
			ok, _ = doublestar.Match(r.pattern, name)
		} else {
			ok, _ = doublestar.Match(r.pattern, rel)
		}

		if ok {
			matched = !r.negate
		}
	}
	return matched
}

// isGitIgnored returns true if the file is ignored by git.
func isGitIgnored(ctx context.Context, absPath string) bool {
	cmd := exec.CommandContext(ctx, "git", "check-ignore", "-q", absPath)
	cmd.Dir = filepath.Dir(absPath)
	err := cmd.Run()
	// Exit code 0 = ignored, 1 = not ignored, other = git not available (allow)
	return err == nil
}
