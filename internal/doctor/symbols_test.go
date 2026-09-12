package doctor

import (
	"strings"
	"testing"

	"github.com/arnelirobles/baryo-cli/internal/index"
)

func TestSymbolCheckReflectsBuild(t *testing.T) {
	r := symbolCheck()
	if index.SymbolsAvailable {
		if !r.Passed {
			t.Errorf("this build extracts symbols, but the check reports %+v", r)
		}
		return
	}
	if r.Passed {
		t.Error("this build cannot extract symbols, so the check must not pass")
	}
	if !strings.Contains(r.Message, "CGO") {
		t.Errorf("message should say why: %q", r.Message)
	}
}

// Missing symbols degrade the repo map to a file list. That is worth telling the
// user about and must never stop baryo from starting.
func TestSymbolCheckNeverBlocksStartup(t *testing.T) {
	if !AllPassed([]CheckResult{symbolCheck()}) {
		t.Error("the symbol check must be informational, not blocking")
	}
}
