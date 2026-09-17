// SPDX-License-Identifier: MIT

package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writePlugin creates a plugin directory under root and returns its path.
func writePlugin(t *testing.T, root, name, manifest string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, body := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const goodManifest = `name: good
version: 1.0.0
description: a working plugin
provides:
  - kind: exporter
    id: good-fmt
    command: ./run.sh
permissions:
  sessions: read
  network: none
`

func TestLoadManifestAcceptsAWorkingPlugin(t *testing.T) {
	dir := writePlugin(t, t.TempDir(), "good", goodManifest, map[string]string{"run.sh": "#!/bin/sh\n"})

	m, err := LoadManifest(dir, SourceGlobal)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Name != "good" || m.Version != "1.0.0" {
		t.Errorf("got name %q version %q", m.Name, m.Version)
	}
	if c, ok := m.Capability(KindExporter, "good-fmt"); !ok || c.Command != "./run.sh" {
		t.Errorf("capability lookup failed: %+v %v", c, ok)
	}
	if got := m.Kinds(); len(got) != 1 || got[0] != KindExporter {
		t.Errorf("Kinds() = %v", got)
	}
}

// A plugin may only run a file it ships. This is the security boundary: without it a
// cloned repository could name /bin/sh, or a path climbing out of its own directory, and
// have Baryo run it.
func TestLoadManifestRefusesCommandsOutsideThePluginDir(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    string
	}{
		{"absolute", "/bin/sh", "must be relative"},
		{"parent traversal", "../../../../bin/sh", "outside the plugin directory"},
		{"empty", "", "command is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := "name: p\nprovides:\n  - kind: exporter\n    id: x\n    command: " + tc.command + "\n"
			dir := writePlugin(t, t.TempDir(), "p", manifest, nil)
			_, err := LoadManifest(dir, SourceGlobal)
			if err == nil {
				t.Fatalf("command %q was accepted", tc.command)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A symlink inside the plugin directory pointing out of it would pass a textual prefix
// check, which is why the paths are resolved before being compared.
func TestLoadManifestRefusesSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := t.TempDir()
	dir := writePlugin(t, root, "p", "name: p\nprovides:\n  - kind: exporter\n    id: x\n    command: ./shell\n", nil)

	outside := filepath.Join(root, "outside.sh")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "shell")); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(dir, SourceGlobal)
	if err == nil {
		t.Fatal("a symlink out of the plugin directory was accepted")
	}
	if !strings.Contains(err.Error(), "outside the plugin directory") {
		t.Errorf("error %q does not explain the refusal", err)
	}
}

// An unknown field is an error rather than a setting that silently does nothing. A plugin
// that believes it declared `network: none` and was ignored is worse than one that refuses
// to load.
func TestLoadManifestRefusesUnknownFields(t *testing.T) {
	manifest := goodManifest + "  netwrok: none\n"
	dir := writePlugin(t, t.TempDir(), "good", manifest, map[string]string{"run.sh": "#!/bin/sh\n"})
	if _, err := LoadManifest(dir, SourceGlobal); err == nil {
		t.Fatal("a misspelled field was accepted")
	}
}

func TestLoadManifestRefusesUnusableManifests(t *testing.T) {
	cases := map[string]string{
		"no name":          "provides:\n  - kind: exporter\n    id: x\n    command: ./run.sh\n",
		"uppercase name":   "name: Good\nprovides:\n  - kind: exporter\n    id: x\n    command: ./run.sh\n",
		"provides nothing": "name: good\n",
		"unknown kind":     "name: good\nprovides:\n  - kind: telepathy\n    id: x\n    command: ./run.sh\n",
		"reserved kind":    "name: good\nprovides:\n  - kind: tool\n    id: x\n    command: ./run.sh\n",
		"bad id":           "name: good\nprovides:\n  - kind: exporter\n    id: \"Not An Id\"\n    command: ./run.sh\n",
		"duplicate":        "name: good\nprovides:\n  - kind: exporter\n    id: x\n    command: ./run.sh\n  - kind: exporter\n    id: x\n    command: ./run.sh\n",
	}
	for name, manifest := range cases {
		t.Run(name, func(t *testing.T) {
			dir := writePlugin(t, t.TempDir(), "good", manifest, map[string]string{"run.sh": "#!/bin/sh\n"})
			if _, err := LoadManifest(dir, SourceGlobal); err == nil {
				t.Errorf("manifest was accepted:\n%s", manifest)
			}
		})
	}
}

// A project plugin arrives with a checkout and runs an executable from it, so it must not
// load until the project is trusted — the same gate that covers project config and skills.
func TestDiscoverGatesProjectPluginsOnTrust(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workdir := t.TempDir()

	writePlugin(t, filepath.Join(home, ".baryo", "plugins"), "good", goodManifest, map[string]string{"run.sh": "#!/bin/sh\n"})
	projectManifest := strings.Replace(goodManifest, "name: good", "name: theirs", 1)
	projectManifest = strings.Replace(projectManifest, "id: good-fmt", "id: their-fmt", 1)
	writePlugin(t, ProjectDir(workdir), "theirs", projectManifest, map[string]string{"run.sh": "#!/bin/sh\n"})

	untrusted, errs := Discover(workdir, false)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(untrusted) != 1 || untrusted[0].Name != "good" {
		t.Fatalf("untrusted discovery loaded %+v, want only the global plugin", untrusted)
	}
	if _, _, ok := FindExporter(untrusted, "their-fmt"); ok {
		t.Error("a project plugin's exporter was usable without trust")
	}
	// The user has to be able to find out that it exists.
	if skipped := UntrustedProjectPlugins(workdir); len(skipped) != 1 || skipped[0] != "theirs" {
		t.Errorf("UntrustedProjectPlugins() = %v, want [theirs]", skipped)
	}

	trusted, errs := Discover(workdir, true)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors when trusted: %v", errs)
	}
	if len(trusted) != 2 {
		t.Fatalf("trusted discovery loaded %d plugins, want 2", len(trusted))
	}
	if _, _, ok := FindExporter(trusted, "their-fmt"); !ok {
		t.Error("a trusted project plugin's exporter was not usable")
	}
}

// A project plugin must not be able to take a global plugin's name, or dropping
// .baryo/plugins/<name> into a repository would quietly replace one the user installed.
func TestDiscoverRefusesProjectPluginShadowingGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workdir := t.TempDir()

	writePlugin(t, filepath.Join(home, ".baryo", "plugins"), "good", goodManifest, map[string]string{"run.sh": "#!/bin/sh\n"})
	writePlugin(t, ProjectDir(workdir), "good", goodManifest, map[string]string{"run.sh": "#!/bin/sh\n"})

	manifests, errs := Discover(workdir, true)
	if len(manifests) != 1 || manifests[0].Source != SourceGlobal {
		t.Fatalf("got %+v, want only the global plugin", manifests)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "already uses that name") {
		t.Errorf("errors = %v, want one name clash", errs)
	}
}

// One broken plugin must not hide the working ones, and must not pass unnoticed.
func TestDiscoverReportsBrokenPluginsAndKeepsGoing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	global := filepath.Join(home, ".baryo", "plugins")

	writePlugin(t, global, "good", goodManifest, map[string]string{"run.sh": "#!/bin/sh\n"})
	writePlugin(t, global, "broken", "name: broken\nprovides:\n  - kind: exporter\n    id: b\n    command: /bin/sh\n", nil)
	// A directory with no manifest is not a plugin and is not an error either.
	if err := os.MkdirAll(filepath.Join(global, "notaplugin"), 0o755); err != nil {
		t.Fatal(err)
	}

	manifests, errs := Discover(t.TempDir(), false)
	if len(manifests) != 1 || manifests[0].Name != "good" {
		t.Fatalf("got %+v, want the working plugin", manifests)
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one", errs)
	}
}

// A missing plugins directory is the normal case, not something to complain about.
func TestDiscoverWithNoPluginsIsQuiet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manifests, errs := Discover(t.TempDir(), true)
	if len(manifests) != 0 || len(errs) != 0 {
		t.Errorf("got %+v, %v; want nothing", manifests, errs)
	}
}

func TestRunExporterReturnsWhatThePluginReports(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test plugin is a shell script")
	}
	script := "#!/bin/sh\ncat >/dev/null\n" +
		`printf '{"ok":true,"written":["/tmp/out.jsonl"],"sessions":2,"events":7,"warnings":["partial"]}'` + "\n"
	dir := writePlugin(t, t.TempDir(), "good", goodManifest, map[string]string{"run.sh": script})

	m, err := LoadManifest(dir, SourceGlobal)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := m.Capability(KindExporter, "good-fmt")

	resp, err := RunExporter(context.Background(), m, c, ExportRequest{OutDir: t.TempDir()})
	if err != nil {
		t.Fatalf("RunExporter: %v", err)
	}
	if !resp.OK || resp.Sessions != 2 || resp.Events != 7 {
		t.Errorf("response = %+v", resp)
	}
	if len(resp.Warnings) != 1 || resp.Warnings[0] != "partial" {
		t.Errorf("warnings = %v", resp.Warnings)
	}
}

// The request has to reach the plugin on stdin, or none of this works.
func TestRunExporterPassesTheRequestOnStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test plugin is a shell script")
	}
	out := t.TempDir()
	// The plugin saves what it was given, and the test reads the file. Echoing JSON back
	// through a shell script means quoting JSON in a shell string, which tests the quoting
	// rather than the contract.
	script := "#!/bin/sh\ncat > \"$1\"\nprintf '{\"ok\":true}'\n"
	dir := writePlugin(t, t.TempDir(), "good", goodManifest, map[string]string{"run.sh": script})

	m, err := LoadManifest(dir, SourceGlobal)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := m.Capability(KindExporter, "good-fmt")
	saved := filepath.Join(out, "request.json")
	c.Args = []string{saved}

	if _, err := RunExporter(context.Background(), m, c, ExportRequest{
		OutDir:      out,
		SessionsDir: "/sessions/dir",
		Sessions:    []SessionRef{{ID: "abc123", MessagesPath: "/sessions/dir/abc123.json"}},
	}); err != nil {
		t.Fatalf("RunExporter: %v", err)
	}

	body, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("the plugin received no request: %v", err)
	}
	var got ExportRequest
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("the request was not valid JSON: %v\n%s", err, body)
	}
	if got.Schema != ExportSchema {
		t.Errorf("schema = %q, want %q", got.Schema, ExportSchema)
	}
	if got.SessionsDir != "/sessions/dir" || got.OutDir != out {
		t.Errorf("paths did not survive: %+v", got)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].ID != "abc123" {
		t.Errorf("sessions did not survive: %+v", got.Sessions)
	}
}

// Three different failures need three different answers, so they must not collapse into
// one opaque error.
func TestRunExporterDistinguishesFailureModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test plugins are shell scripts")
	}
	cases := []struct {
		name   string
		script string
		want   string
	}{
		{
			name:   "reported failure",
			script: "#!/bin/sh\ncat >/dev/null\nprintf '{\"ok\":false,\"error\":\"unsupported schema\"}'\n",
			want:   "unsupported schema",
		},
		{
			name:   "no usable output",
			script: "#!/bin/sh\ncat >/dev/null\necho not json\n",
			want:   "no usable response",
		},
		{
			name:   "crashed with stderr",
			script: "#!/bin/sh\ncat >/dev/null\necho 'disk full' >&2\nexit 3\n",
			want:   "disk full",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writePlugin(t, t.TempDir(), "good", goodManifest, map[string]string{"run.sh": tc.script})
			m, _ := LoadManifest(dir, SourceGlobal)
			c, _ := m.Capability(KindExporter, "good-fmt")

			_, err := RunExporter(context.Background(), m, c, ExportRequest{OutDir: t.TempDir()})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}
