// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package llm

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Hardware describes the system's memory and processor.
type Hardware struct {
	TotalRAM   uint64 // bytes
	UnifiedMem bool   // Apple Silicon unified memory
	ChipName   string // e.g. "Apple M2 Pro"
}

const fallbackRAM = 8 * 1024 * 1024 * 1024 // 8 GiB

// DetectHardware returns system hardware info.
// Falls back to 8 GiB if detection fails.
func DetectHardware() Hardware {
	switch runtime.GOOS {
	case "darwin":
		return detectDarwin()
	case "linux":
		return detectLinux()
	default:
		return Hardware{TotalRAM: fallbackRAM}
	}
}

func detectDarwin() Hardware {
	hw := Hardware{TotalRAM: fallbackRAM}

	// Total RAM via sysctl.
	if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		if v, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64); err == nil {
			hw.TotalRAM = v
		}
	}

	// Chip name via sysctl.
	if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
		hw.ChipName = strings.TrimSpace(string(out))
	}

	// Apple Silicon has unified memory.
	hw.UnifiedMem = strings.Contains(hw.ChipName, "Apple")

	return hw
}

func detectLinux() Hardware {
	hw := Hardware{TotalRAM: fallbackRAM}

	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return hw
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if v, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					hw.TotalRAM = v * 1024 // /proc/meminfo reports in kB
				}
			}
			break
		}
	}

	return hw
}
