// SPDX-License-Identifier: MIT

package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// GlobalDir returns ~/.baryo/plugins, or "" when there is no home directory.
func GlobalDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".baryo", "plugins")
}

// ProjectDir returns the project plugin directory for a working directory.
func ProjectDir(workdir string) string {
	return filepath.Join(workdir, ".baryo", "plugins")
}

// Discover finds installed plugins.
//
// Global plugins (~/.baryo/plugins) are always loaded: the user put them there. Project
// plugins (.baryo/plugins) arrive with a checkout and are loaded only when the project is
// trusted — the same gate that already covers a project's config, skills and hooks, which
// is the weakest thing a project plugin could be mistaken for. A plugin ships an
// executable that Baryo runs, so it cannot have an easier path than a skill does.
//
// Returns the manifests it could load and one error per directory it could not, because a
// single broken plugin must not hide the working ones. Names are unique: a project plugin
// cannot shadow a global one of the same name, so dropping a .baryo/plugins/git into a
// repository cannot quietly replace a plugin the user installed.
func Discover(workdir string, projectTrusted bool) ([]Manifest, []error) {
	var manifests []Manifest
	var errs []error

	taken := make(map[string]Source)

	add := func(dir string, src Source) {
		found, loadErrs := loadDir(dir, src)
		errs = append(errs, loadErrs...)
		for _, m := range found {
			if owner, clash := taken[m.Name]; clash {
				errs = append(errs, fmt.Errorf("%s plugin %q ignored: a %s plugin already uses that name",
					src, m.Name, owner))
				continue
			}
			taken[m.Name] = src
			manifests = append(manifests, m)
		}
	}

	if dir := GlobalDir(); dir != "" {
		add(dir, SourceGlobal)
	}
	if projectTrusted && workdir != "" {
		add(ProjectDir(workdir), SourceProject)
	}

	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Name < manifests[j].Name })
	return manifests, errs
}

// UntrustedProjectPlugins reports the names of project plugins that were skipped because
// the project is not trusted, so the user can be told they exist rather than left
// wondering why nothing loaded.
func UntrustedProjectPlugins(workdir string) []string {
	entries, err := os.ReadDir(ProjectDir(workdir))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(ProjectDir(workdir), e.Name(), ManifestName)); err == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// loadDir loads every plugin directory directly under dir.
func loadDir(dir string, src Source) ([]Manifest, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// A missing plugins directory is the normal case, not a problem to report.
		return nil, nil
	}
	var manifests []Manifest
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(sub, ManifestName)); err != nil {
			continue // a directory without a manifest is not a plugin
		}
		m, err := LoadManifest(sub, src)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if m.Name != e.Name() {
			errs = append(errs, fmt.Errorf("%s: plugin name %q does not match its directory %q",
				filepath.Join(sub, ManifestName), m.Name, e.Name()))
			continue
		}
		manifests = append(manifests, m)
	}
	return manifests, errs
}

// FindExporter returns the plugin and capability providing an exporter id.
func FindExporter(manifests []Manifest, id string) (Manifest, Capability, bool) {
	for _, m := range manifests {
		if c, ok := m.Capability(KindExporter, id); ok {
			return m, c, true
		}
	}
	return Manifest{}, Capability{}, false
}
