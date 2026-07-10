// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

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
func IsIgnored(ctx context.Context, absPath string) bool {
	name := filepath.Base(absPath)

	// 1. Builtin patterns (match basename only).
	for _, pat := range builtinPatterns {
		if ok, _ := doublestar.Match(pat, name); ok {
			return true
		}
	}

	// 2. .baryoignore rules.
	rules := loadRules()
	if matchRules(rules, absPath) {
		return true
	}

	// 3. Git check-ignore fallback.
	return isGitIgnored(ctx, absPath)
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
