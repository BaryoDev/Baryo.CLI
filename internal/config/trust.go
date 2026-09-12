// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/fsutil"
)

// Project trust. A project's own .baryo/config.yaml can start processes (hooks,
// mcp_servers, ssh_tunnel), change the permission mode and replace the system
// prompt, so none of it applies until the user trusts that directory.
//
// Trust is recorded per resolved directory under ~/.baryo/trusted/.

// projectTrusted is the process-wide answer to "did the user trust this
// working directory". Set once at startup by Load. It lives here rather than
// being threaded through every call site so that no call site can forget it.
var projectTrusted bool

// SetProjectTrusted records the trust decision for this process.
func SetProjectTrusted(trusted bool) { projectTrusted = trusted }

// ProjectTrusted reports the trust decision for this process.
func ProjectTrusted() bool { return projectTrusted }

// ProjectConfigPresent reports whether dir carries project-level Baryo config
// or skills, which is what makes a trust decision necessary.
func ProjectConfigPresent(dir string) bool {
	for _, p := range []string{
		filepath.Join(dir, ".baryo", "config.yaml"),
		filepath.Join(dir, ".baryo", "skills"),
		filepath.Join(dir, "skills"),
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// trustKey identifies a directory by its fully resolved path, so a symlinked
// checkout is not a second identity for the same project.
func trustKey(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])
}

// trustFile returns the marker path for a directory, or "" with no home dir.
func trustFile(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".baryo", "trusted", trustKey(dir))
}

// IsProjectTrusted reports whether the user has trusted this directory.
func IsProjectTrusted(dir string) bool {
	path := trustFile(dir)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// TrustProject records the user's decision to trust this directory.
func TrustProject(dir string) error {
	path := trustFile(dir)
	if path == "" {
		return errNoHomeDir
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	abs, _ := filepath.Abs(dir)
	body := abs + "\n" + time.Now().UTC().Format(time.RFC3339) + "\n"
	return fsutil.WriteFileAtomic(path, []byte(body), 0o600)
}

type trustError string

func (e trustError) Error() string { return string(e) }

const errNoHomeDir trustError = "cannot determine home directory"
