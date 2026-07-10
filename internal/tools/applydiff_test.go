// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tools

import (
	"strings"
	"testing"
)

func TestParseUnifiedDiff(t *testing.T) {
	diff := `--- a/file.go
+++ b/file.go
@@ -1,3 +1,3 @@
 package main
-import "fmt"
+import "log"
 func main() {`

	hunks, err := parseUnifiedDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(hunks))
	}
	if hunks[0].oldStart != 1 || hunks[0].oldCount != 3 {
		t.Errorf("hunk range: got start=%d count=%d, want start=1 count=3",
			hunks[0].oldStart, hunks[0].oldCount)
	}
}

func TestApplyHunksSingleHunk(t *testing.T) {
	lines := []string{"package main", "", `import "fmt"`, "", "func main() {", `	fmt.Println("hello")`, "}"}

	diff := `@@ -3,1 +3,1 @@
-import "fmt"
+import "log"`

	hunks, err := parseUnifiedDiff(diff)
	if err != nil {
		t.Fatal(err)
	}

	result, err := applyHunks(lines, hunks)
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(result, "\n")
	if !strings.Contains(joined, `import "log"`) {
		t.Error("expected import to be replaced")
	}
	if strings.Contains(joined, `import "fmt"`) {
		t.Error("old import should be removed")
	}
}

func TestApplyHunksMultiHunk(t *testing.T) {
	lines := []string{"line1", "line2", "line3", "line4", "line5"}

	diff := `@@ -2,1 +2,1 @@
-line2
+LINE2
@@ -4,1 +4,1 @@
-line4
+LINE4`

	hunks, err := parseUnifiedDiff(diff)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 2 {
		t.Fatalf("got %d hunks, want 2", len(hunks))
	}

	result, err := applyHunks(lines, hunks)
	if err != nil {
		t.Fatal(err)
	}

	if result[1] != "LINE2" {
		t.Errorf("line 2 = %q, want %q", result[1], "LINE2")
	}
	if result[3] != "LINE4" {
		t.Errorf("line 4 = %q, want %q", result[3], "LINE4")
	}
}

func TestApplyHunksContextMismatch(t *testing.T) {
	lines := []string{"line1", "line2", "line3"}

	diff := `@@ -1,2 +1,2 @@
 wrong_context
-line2
+LINE2`

	hunks, err := parseUnifiedDiff(diff)
	if err != nil {
		t.Fatal(err)
	}

	_, err = applyHunks(lines, hunks)
	if err == nil {
		t.Error("expected context mismatch error")
	}
	if !strings.Contains(err.Error(), "context mismatch") {
		t.Errorf("error = %q, want context mismatch", err.Error())
	}
}

func TestParseMalformedDiff(t *testing.T) {
	_, err := parseUnifiedDiff("@@ invalid @@")
	if err == nil {
		t.Error("expected error for malformed hunk header")
	}
}

func TestApplyHunkInsertionBeyondEOF(t *testing.T) {
	lines := []string{"line1", "line2"}

	// Pure-insertion hunk targeting a line far past the end of the file.
	diff := `@@ -50,0 +50,2 @@
+new line A
+new line B`

	hunks, err := parseUnifiedDiff(diff)
	if err != nil {
		t.Fatal(err)
	}

	_, err = applyHunks(lines, hunks)
	if err == nil {
		t.Fatal("expected error for hunk beyond end of file")
	}
	if !strings.Contains(err.Error(), "beyond end of file") {
		t.Errorf("error = %q, want beyond end of file", err.Error())
	}
}

func TestParseBareHunkHeader(t *testing.T) {
	_, err := parseUnifiedDiff("@@")
	if err == nil {
		t.Error("expected error for bare @@ header")
	}
}

func TestParseRangeNoCount(t *testing.T) {
	start, count, err := parseRange("5")
	if err != nil {
		t.Fatal(err)
	}
	if start != 5 || count != 1 {
		t.Errorf("got start=%d count=%d, want start=5 count=1", start, count)
	}
}
