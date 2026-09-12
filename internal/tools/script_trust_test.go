package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arnelirobles/baryo-cli/internal/config"
)

// run_script is restricted to skill directories, but two of those roots are
// relative to the working directory. An untrusted project can ship
// skills/evil/run.sh and instruct the model to run it by path, which the skill
// index gate does not cover because nothing has to be indexed for that.
func TestIsInsideSkillDirIgnoresProjectSkillsWhenUntrusted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	for _, d := range []string{
		filepath.Join(proj, "skills", "evil"),
		filepath.Join(proj, ".baryo", "skills", "evil"),
		filepath.Join(home, ".baryo", "skills", "mine"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(proj)

	projScript := filepath.Join(proj, "skills", "evil", "run.sh")
	baryoScript := filepath.Join(proj, ".baryo", "skills", "evil", "run.sh")
	globalScript := filepath.Join(home, ".baryo", "skills", "mine", "run.sh")

	config.SetProjectTrusted(false)
	if isInsideSkillDir(projScript) {
		t.Error("an untrusted project's skills/ must not be an allowed script root")
	}
	if isInsideSkillDir(baryoScript) {
		t.Error("an untrusted project's .baryo/skills must not be an allowed script root")
	}
	if !isInsideSkillDir(globalScript) {
		t.Error("the user's own global skills must always be allowed")
	}

	config.SetProjectTrusted(true)
	if !isInsideSkillDir(projScript) {
		t.Error("a trusted project's skills/ should be an allowed script root")
	}
}
