// SPDX-License-Identifier: MIT

//go:build !unix

package procutil

import "os/exec"

// SetProcessGroup is a no-op on platforms without unix process groups.
func SetProcessGroup(cmd *exec.Cmd) {}
