// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const baseURL = "https://raw.githubusercontent.com/arnelirobles/baryo-cli/main/default-skills/"

// Manifest describes the available starter skills.
type Manifest struct {
	Version int             `json:"version"`
	Skills  []ManifestSkill `json:"skills"`
}

// ManifestSkill describes a single skill in the manifest.
type ManifestSkill struct {
	Name  string   `json:"name"`
	Files []string `json:"files"`
}

// DownloadProgress reports progress for a single skill download.
type DownloadProgress struct {
	Skill   string
	Current int
	Total   int
	Done    bool
	Err     error
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// skillsDir returns the path to ~/.baryo/skills/.
func skillsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".baryo", "skills")
}

// stampPath returns the path to the setup stamp file.
func stampPath() string {
	return filepath.Join(skillsDir(), ".setup-stamp")
}

// NeedsSetup returns true if the user has never run setup:
// ~/.baryo/skills/ is missing or empty, and no .setup-stamp exists.
func NeedsSetup() bool {
	dir := skillsDir()

	// If stamp exists, user already ran or declined setup.
	if _, err := os.Stat(stampPath()); err == nil {
		return false
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist.
		return true
	}

	// Check if there are any skill directories (ignore dotfiles).
	for _, e := range entries {
		if e.IsDir() && e.Name()[0] != '.' {
			return false
		}
	}
	return true
}

// FetchManifest downloads and parses the manifest from GitHub.
func FetchManifest() (*Manifest, error) {
	resp, err := httpClient.Get(baseURL + "manifest.json")
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch manifest: HTTP %d", resp.StatusCode)
	}

	var m Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// DownloadSkills downloads each skill from the manifest, sending progress
// updates on the returned channel. Skills with user modifications are skipped.
func DownloadSkills(manifest *Manifest) <-chan DownloadProgress {
	ch := make(chan DownloadProgress, len(manifest.Skills)+1)
	total := len(manifest.Skills)

	go func() {
		defer close(ch)
		dir := skillsDir()

		for i, skill := range manifest.Skills {
			ch <- DownloadProgress{
				Skill:   skill.Name,
				Current: i + 1,
				Total:   total,
			}

			skillDir := filepath.Join(dir, skill.Name)
			if err := os.MkdirAll(skillDir, 0o755); err != nil {
				ch <- DownloadProgress{Skill: skill.Name, Err: fmt.Errorf("mkdir: %w", err), Done: true}
				return
			}

			for _, file := range skill.Files {
				localPath := filepath.Join(skillDir, file)

				// Check if user has modified this file.
				if isUserModified(localPath) {
					continue // skip user-modified files
				}

				url := baseURL + skill.Name + "/" + file
				content, err := fetchFile(url)
				if err != nil {
					ch <- DownloadProgress{Skill: skill.Name, Err: err, Done: true}
					return
				}

				if err := os.WriteFile(localPath, content, 0o644); err != nil {
					ch <- DownloadProgress{Skill: skill.Name, Err: err, Done: true}
					return
				}

				// Write checksum alongside the file.
				writeChecksum(localPath, content)
			}
		}

		WriteStamp()
		ch <- DownloadProgress{Done: true, Current: total, Total: total}
	}()

	return ch
}

// WriteStamp writes the setup stamp file to mark setup as done or declined.
func WriteStamp() {
	dir := skillsDir()
	os.MkdirAll(dir, 0o755)
	os.WriteFile(stampPath(), []byte("done\n"), 0o644)
}

// fetchFile downloads a single file from a URL and returns its content.
func fetchFile(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return data, nil
}

// checksumPath returns the path to a file's checksum sidecar.
func checksumPath(filePath string) string {
	return filePath + ".checksum"
}

// writeChecksum writes a SHA-256 checksum file alongside the given file.
func writeChecksum(filePath string, content []byte) {
	hash := sha256.Sum256(content)
	hex := hex.EncodeToString(hash[:])
	os.WriteFile(checksumPath(filePath), []byte(hex), 0o644)
}

// isUserModified returns true if the file exists and has been modified
// since the last download (checksum mismatch).
func isUserModified(filePath string) bool {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false // file doesn't exist, safe to write
	}

	stored, err := os.ReadFile(checksumPath(filePath))
	if err != nil {
		return true // no checksum file but file exists — assume user created it
	}

	hash := sha256.Sum256(content)
	current := hex.EncodeToString(hash[:])
	return current != string(stored)
}
