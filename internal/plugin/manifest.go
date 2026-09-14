// SPDX-License-Identifier: MIT

// Package plugin loads and runs Baryo plugins.
//
// A plugin is a directory with a plugin.yaml and, usually, an executable. Baryo speaks to
// it as a separate process over stdin and stdout with JSON, rather than loading it into
// this one.
//
// That is a deliberate choice against Go's plugin package: -buildmode=plugin requires cgo,
// requires the host and the plugin to be built with the same toolchain and byte-identical
// versions of every shared dependency, and does not work on Windows. Baryo's released
// binaries are static CGO_ENABLED=0 builds precisely so they run anywhere, and a shared
// object ABI would give that up and replace it with "your plugin was built with Go 1.25.1,
// this binary is 1.25.0". A subprocess costs one spawn and works in any language.
//
// See design/specs/2026-09-13-baryo-plugin-architecture.md.
package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestName is the file that marks a plugin directory.
const ManifestName = "plugin.yaml"

// Capability kinds. Only KindExporter is implemented; the rest are named here because the
// manifest format is the part that has to be stable, and a plugin author needs to know
// which words are reserved before writing one.
const (
	KindExporter = "exporter" // reads sessions and writes some other format
	KindTool     = "tool"     // reserved
	KindHook     = "hook"     // reserved
	KindSkill    = "skill"    // reserved
	KindProvider = "provider" // reserved
)

// Source says where a plugin was found, which decides how much it is trusted.
type Source string

const (
	// SourceGlobal is ~/.baryo/plugins. The user put it there themselves.
	SourceGlobal Source = "global"
	// SourceProject is .baryo/plugins in the working directory. It arrives with a
	// checkout, from whoever wrote the repository, so it needs an explicit decision.
	SourceProject Source = "project"
)

// Manifest is a plugin's plugin.yaml, plus where it was found.
type Manifest struct {
	Name        string       `yaml:"name"`
	Version     string       `yaml:"version"`
	Description string       `yaml:"description"`
	Provides    []Capability `yaml:"provides"`
	Permissions Permissions  `yaml:"permissions"`

	Dir    string `yaml:"-"` // directory holding plugin.yaml
	Source Source `yaml:"-"` // global or project
}

// Capability is one thing a plugin can do. Everything a plugin may do is declared here:
// nothing is registered at run time, so `baryo plugins list` can say what a plugin does
// without running it, and a reviewer can read it.
type Capability struct {
	Kind    string   `yaml:"kind"`
	ID      string   `yaml:"id"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

// Permissions is what a plugin says it needs.
//
// Declared, not yet enforced. It is recorded and displayed so a user can see what they are
// agreeing to, and so the fields exist before there are plugins to migrate — but nothing
// here currently constrains the subprocess, and the docs must not imply otherwise.
type Permissions struct {
	Sessions   string `yaml:"sessions"`
	Network    string `yaml:"network"`
	Filesystem string `yaml:"filesystem"`
}

// validName keeps a plugin name usable as a directory name, a CLI argument and a log field.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// validID is the same shape for capability ids, which are what `--format` matches against.
var validID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// LoadManifest reads and validates the plugin.yaml in dir.
func LoadManifest(dir string, src Source) (Manifest, error) {
	path := filepath.Join(dir, ManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	// KnownFields so a typo in a field name is an error rather than a setting that
	// silently does nothing. A plugin that believes it declared `network: none` and was
	// ignored is worse than one that fails to load.
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", path, err)
	}
	m.Dir = dir
	m.Source = src
	if err := m.validate(); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// validate rejects a manifest Baryo should not act on.
func (m *Manifest) validate() error {
	if !validName.MatchString(m.Name) {
		return fmt.Errorf("name %q must be lowercase letters, digits, dot, dash or underscore", m.Name)
	}
	if len(m.Provides) == 0 {
		return fmt.Errorf("plugin %q provides nothing", m.Name)
	}
	seen := make(map[string]bool, len(m.Provides))
	for i := range m.Provides {
		c := &m.Provides[i]
		switch c.Kind {
		case KindExporter:
			// Implemented below.
		case KindTool, KindHook, KindSkill, KindProvider:
			return fmt.Errorf("capability kind %q is reserved but not implemented yet", c.Kind)
		default:
			return fmt.Errorf("unknown capability kind %q", c.Kind)
		}
		if !validID.MatchString(c.ID) {
			return fmt.Errorf("capability id %q must be lowercase letters, digits, dot, dash or underscore", c.ID)
		}
		key := c.Kind + "/" + c.ID
		if seen[key] {
			return fmt.Errorf("duplicate capability %s", key)
		}
		seen[key] = true
		if _, err := m.resolveCommand(c.Command); err != nil {
			return fmt.Errorf("capability %s: %w", key, err)
		}
	}
	return nil
}

// resolveCommand turns a manifest command into an absolute path inside the plugin
// directory, refusing anything that points outside it.
//
// This is the security boundary, and it matters most for a project plugin: without it, a
// cloned repository could ship a manifest whose command is /bin/sh or ../../.ssh/something
// and have Baryo run it. A plugin may only execute a file it actually ships, so reviewing
// a plugin means reviewing that directory and nothing else.
//
// Symlinks are resolved before the check, because a symlink inside the directory pointing
// out of it would otherwise pass a textual prefix test.
func (m *Manifest) resolveCommand(command string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command is empty")
	}
	if filepath.IsAbs(command) {
		return "", fmt.Errorf("command %q must be relative to the plugin directory", command)
	}

	root, err := filepath.Abs(m.Dir)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}

	full := filepath.Join(root, filepath.FromSlash(command))
	if resolved, err := filepath.EvalSymlinks(full); err == nil {
		full = resolved
	}

	// filepath.Rel rather than a string prefix: a sibling directory whose name starts
	// with the root's name would satisfy a prefix test.
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("command %q resolves outside the plugin directory", command)
	}

	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("command %q: %w", command, err)
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return "", fmt.Errorf("command %q is not a regular file", command)
	}
	return full, nil
}

// Capability returns the capability with the given kind and id.
func (m Manifest) Capability(kind, id string) (Capability, bool) {
	for _, c := range m.Provides {
		if c.Kind == kind && c.ID == id {
			return c, true
		}
	}
	return Capability{}, false
}

// Kinds lists the capability kinds a plugin provides, for display.
func (m Manifest) Kinds() []string {
	var kinds []string
	seen := map[string]bool{}
	for _, c := range m.Provides {
		if !seen[c.Kind] {
			seen[c.Kind] = true
			kinds = append(kinds, c.Kind)
		}
	}
	return kinds
}
