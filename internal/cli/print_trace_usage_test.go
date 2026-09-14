// SPDX-License-Identifier: MIT

package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/arnelirobles/baryo-cli/internal/llm"
)

// The benchmark runner reads token cost from the task_end trace record, so
// headless mode must write what the provider reported rather than zeros.
func TestHeadlessTraceRecordsReportedTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"choices":[{"delta":{"content":"hello"},"finish_reason":"stop"}]}`+"\n\n")
		io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":321,"completion_tokens":45,"total_tokens":366}}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	for name, run := range map[string]func(PrintOptions) int{"text": runPrintText, "json": runPrintJSON} {
		t.Run(name, func(t *testing.T) {
			tracePath := filepath.Join(t.TempDir(), "trace.jsonl")
			stdout := os.Stdout
			devnull, _ := os.Open(os.DevNull)
			os.Stdout = devnull
			code := run(PrintOptions{
				Endpoint:  llm.Endpoint{BaseURL: srv.URL + "/v1", Provider: "openai", APIKey: "test"},
				Model:     llm.Model{Tag: "test-model"},
				Prompt:    "hi",
				TraceFile: tracePath,
			})
			os.Stdout = stdout
			devnull.Close()
			if code != 0 {
				t.Fatalf("exit %d", code)
			}

			f, err := os.Open(tracePath)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			var end struct {
				T     string         `json:"t"`
				Usage map[string]int `json:"usage"`
			}
			found := false
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				var rec struct {
					T string `json:"t"`
				}
				if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.T == "task_end" {
					json.Unmarshal(sc.Bytes(), &end)
					found = true
				}
			}
			if !found {
				t.Fatal("trace has no task_end record")
			}
			if end.Usage["prompt"] != 321 || end.Usage["completion"] != 45 {
				t.Errorf("task_end usage = %v, want prompt 321 and completion 45", end.Usage)
			}
		})
	}
}
