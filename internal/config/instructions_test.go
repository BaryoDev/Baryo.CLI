package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func instructionsProject(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".baryo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".baryo", "BARYO.md"), []byte("GLOBAL RULES"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "BARYO.md"), []byte("PROJECT RULES"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(proj)
	return proj
}

// The user's own global file is theirs and always applies.
func TestGlobalInstructionsAlwaysLoaded(t *testing.T) {
	instructionsProject(t)
	SetProjectTrusted(false)
	got := LoadProjectInstructions(false)
	if !strings.Contains(got, "GLOBAL RULES") {
		t.Errorf("global instructions missing from %q", got)
	}
}

// Untrusted project instructions still load, because that is what the tool is
// for, but they are labelled so the model is told what they are.
func TestUntrustedProjectInstructionsAreLabelled(t *testing.T) {
	instructionsProject(t)
	SetProjectTrusted(false)
	got := LoadProjectInstructions(true)
	if !strings.Contains(got, "PROJECT RULES") {
		t.Errorf("project instructions missing from %q", got)
	}
	if !strings.Contains(got, "untrusted-project-instructions") {
		t.Errorf("project instructions were not labelled untrusted: %q", got)
	}
}

// Untrusted project text plus auto-approval is the one combination with no
// human in it, so there the project's instructions are refused.
func TestUntrustedProjectInstructionsRefusedWhenNotAllowed(t *testing.T) {
	instructionsProject(t)
	SetProjectTrusted(false)
	got := LoadProjectInstructions(false)
	if strings.Contains(got, "PROJECT RULES") {
		t.Errorf("project instructions should be refused, got %q", got)
	}
}

func TestTrustedProjectInstructionsAreNotLabelled(t *testing.T) {
	instructionsProject(t)
	SetProjectTrusted(true)
	got := LoadProjectInstructions(true)
	if !strings.Contains(got, "PROJECT RULES") {
		t.Errorf("project instructions missing from %q", got)
	}
	if strings.Contains(got, "untrusted-project-instructions") {
		t.Errorf("a trusted project should not be labelled untrusted: %q", got)
	}
}

// ProjectInstructionFiles reports what was read from the project so the user
// can be told, rather than it happening silently.
func TestProjectInstructionFilesReported(t *testing.T) {
	instructionsProject(t)
	SetProjectTrusted(false)
	files := ProjectInstructionFiles()
	if len(files) != 1 || files[0] != "BARYO.md" {
		t.Errorf("got %v, want [BARYO.md]", files)
	}
}
