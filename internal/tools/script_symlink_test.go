package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arnelirobles/baryo-cli/internal/config"
)

// run_script is confined to skill directories, but the check compared the
// literal path, so a symlink inside a skill directory pointed execution
// anywhere. The rule is that the resolved path must sit under a resolved root:
// that rejects an escaping symlink while still allowing a skills directory that
// is itself a symlink, for example one managed in a dotfiles checkout.
func TestIsInsideSkillDirRejectsSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config.SetProjectTrusted(false)

	skillDir := filepath.Join(home, ".baryo", "skills", "mine")
	outside := filepath.Join(home, "outside")
	for _, d := range []string{skillDir, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	real := filepath.Join(skillDir, "run.sh")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "evil.sh")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if !isInsideSkillDir(real) {
		t.Error("a real script inside a skill directory must be allowed")
	}

	link := filepath.Join(skillDir, "evil.sh")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if isInsideSkillDir(link) {
		t.Error("a symlink pointing outside the skill directories must be rejected")
	}

	linkedDir := filepath.Join(skillDir, "scripts")
	if err := os.Symlink(outside, linkedDir); err != nil {
		t.Fatal(err)
	}
	if isInsideSkillDir(filepath.Join(linkedDir, "evil.sh")) {
		t.Error("a script reached through a symlinked subdirectory must be rejected")
	}
}

// A skills directory that is itself a symlink is a normal setup, so resolving
// must not lock those users out.
func TestIsInsideSkillDirAllowsSymlinkedSkillsRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config.SetProjectTrusted(false)

	elsewhere := filepath.Join(home, "dotfiles", "skills", "mine")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(elsewhere, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".baryo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "dotfiles", "skills"), filepath.Join(home, ".baryo", "skills")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	via := filepath.Join(home, ".baryo", "skills", "mine", "run.sh")
	if !isInsideSkillDir(via) {
		t.Error("a script under a symlinked skills root must be allowed")
	}
}

func TestIsInsideSkillDirRejectsTraversal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config.SetProjectTrusted(false)
	if err := os.MkdirAll(filepath.Join(home, ".baryo", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if isInsideSkillDir(filepath.Join(home, ".baryo", "skills", "..", "..", "evil.sh")) {
		t.Error("a traversal out of the skills directory must be rejected")
	}
}
