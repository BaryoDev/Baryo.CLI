// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package config

import (
	"os"
	"path/filepath"
	"strings"
)

// LoadProjectInstructions reads BARYO.md files from standard locations and
// returns their combined content. Skills are loaded separately via SkillIndex().
// Files are optional — missing files are silently skipped.
//
// Load order (all found are concatenated):
//  1. ~/.baryo/BARYO.md    (global user instructions)
//  2. .baryo/BARYO.md      (project config dir)
//  3. BARYO.md             (project root)
func LoadProjectInstructions() string {
	var parts []string

	home, _ := os.UserHomeDir()

	baryoPaths := []string{}
	if home != "" {
		baryoPaths = append(baryoPaths, filepath.Join(home, ".baryo", "BARYO.md"))
	}
	baryoPaths = append(baryoPaths,
		filepath.Join(".baryo", "BARYO.md"),
		"BARYO.md",
	)

	for _, p := range baryoPaths {
		if content := readFileIfExists(p); content != "" {
			parts = append(parts, content)
		}
	}

	// Append skill index (lightweight — names and descriptions only)
	skills := SkillIndex()
	if prompt := FormatSkillIndex(skills); prompt != "" {
		parts = append(parts, prompt)
	}

	return strings.Join(parts, "\n\n")
}

// readFileIfExists reads a file and returns its trimmed content.
// Returns empty string if the file doesn't exist or can't be read.
func readFileIfExists(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
