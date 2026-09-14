// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arnelirobles/baryo-cli/internal/export"
	"github.com/arnelirobles/baryo-cli/internal/plugin"
)

// RunPlugins implements `baryo plugins [list|inspect <name>]`. Returns an exit code.
func RunPlugins(cfg Config, trusted bool) int {
	workdir, _ := os.Getwd()
	manifests, errs := plugin.Discover(workdir, trusted)

	if cfg.PluginsCmd == "inspect" {
		return inspectPlugin(cfg.PluginName, manifests, errs)
	}

	if len(manifests) == 0 {
		fmt.Println("No plugins installed.")
		fmt.Printf("\nInstall one by putting its directory in %s\n", displayPath(plugin.GlobalDir()))
	} else {
		fmt.Printf("%-24s %-10s %-9s %s\n", "NAME", "VERSION", "SOURCE", "PROVIDES")
		for _, m := range manifests {
			fmt.Printf("%-24s %-10s %-9s %s\n", m.Name, orDash(m.Version), m.Source,
				strings.Join(m.Kinds(), ", "))
		}
	}

	// Project plugins that exist but were not loaded are reported rather than ignored:
	// silence here looks identical to "there are none", and the user cannot act on it.
	if !trusted {
		if skipped := plugin.UntrustedProjectPlugins(workdir); len(skipped) > 0 {
			fmt.Printf("\nThis project ships %d plugin(s) that were not loaded: %s\n",
				len(skipped), strings.Join(skipped, ", "))
			fmt.Println("A plugin runs an executable from the repository. Pass --trust-project to load them.")
		}
	}

	reportPluginErrors(errs)
	return 0
}

// inspectPlugin prints one plugin in full, which is what a user should read before
// trusting it.
func inspectPlugin(name string, manifests []plugin.Manifest, errs []error) int {
	if name == "" {
		fmt.Fprintln(os.Stderr, "baryo plugins inspect: needs a plugin name")
		return 2
	}
	for _, m := range manifests {
		if m.Name != name {
			continue
		}
		fmt.Printf("%s %s (%s)\n", m.Name, orDash(m.Version), m.Source)
		if m.Description != "" {
			fmt.Printf("\n%s\n", m.Description)
		}
		fmt.Printf("\nDirectory: %s\n", displayPath(m.Dir))
		fmt.Println("\nProvides:")
		for _, c := range m.Provides {
			fmt.Printf("  %s %s\n", c.Kind, c.ID)
			fmt.Printf("    runs: %s %s\n", c.Command, strings.Join(c.Args, " "))
		}
		fmt.Println("\nDeclared permissions:")
		for _, row := range [][2]string{
			{"sessions", m.Permissions.Sessions},
			{"network", m.Permissions.Network},
			{"filesystem", m.Permissions.Filesystem},
		} {
			fmt.Printf("  %-11s %s\n", row[0], orDash(row[1]))
		}
		// Said out loud, because a permissions block that looks enforced and is not is
		// worse than no permissions block at all.
		fmt.Println("\nPermissions are declared by the plugin and displayed here. They are not")
		fmt.Println("yet enforced: a plugin runs with this process's own access.")
		return 0
	}
	fmt.Fprintf(os.Stderr, "baryo plugins inspect: no plugin named %q\n", name)
	reportPluginErrors(errs)
	return 1
}

// RunExport implements `baryo export --format <id>`. Returns an exit code.
func RunExport(cfg Config, trusted bool) int {
	workdir, _ := os.Getwd()
	manifests, errs := plugin.Discover(workdir, trusted)
	reportPluginErrors(errs)

	format := strings.TrimSpace(cfg.Format)
	if format == "" {
		fmt.Fprintln(os.Stderr, "baryo export: --format is required")
		listFormats(manifests)
		return 2
	}

	since, err := parseSince(cfg.Since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "baryo export: %v\n", err)
		return 2
	}

	sessionsDir, refs, err := export.Gather(since, cfg.SessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "baryo export: %v\n", err)
		return 1
	}
	if len(refs) == 0 {
		fmt.Println("Nothing to export: no sessions matched.")
		return 0
	}

	outDir := cfg.OutDir
	if outDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "baryo export: cannot determine home directory; pass --out")
			return 1
		}
		outDir = filepath.Join(home, ".baryo", "exports", format)
	}
	absOut, err := filepath.Abs(outDir)
	if err == nil {
		outDir = absOut
	}

	req := plugin.ExportRequest{
		Schema:      plugin.ExportSchema,
		BaryoVer:    Version,
		SessionsDir: sessionsDir,
		Sessions:    refs,
		Since:       cfg.Since,
		OutDir:      outDir,
	}

	if format == export.BuiltinID {
		resp, err := export.Builtin(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "baryo export: %v\n", err)
			return 1
		}
		export.Describe(os.Stdout, format, resp)
		return 0
	}

	m, capability, ok := plugin.FindExporter(manifests, format)
	if !ok {
		fmt.Fprintf(os.Stderr, "baryo export: no exporter provides format %q\n", format)
		listFormats(manifests)
		return 2
	}

	fmt.Printf("Exporting %d session(s) with plugin %s...\n", len(refs), m.Name)
	resp, err := plugin.RunExporter(context.Background(), m, capability, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "baryo export: %v\n", err)
		return 1
	}
	export.Describe(os.Stdout, format, resp)
	return 0
}

// listFormats shows what this installation can actually write.
func listFormats(manifests []plugin.Manifest) {
	formats := []string{export.BuiltinID + " (built in)"}
	for _, m := range manifests {
		for _, c := range m.Provides {
			if c.Kind == plugin.KindExporter {
				formats = append(formats, fmt.Sprintf("%s (plugin %s)", c.ID, m.Name))
			}
		}
	}
	sort.Strings(formats)
	fmt.Fprintln(os.Stderr, "\nAvailable formats:")
	for _, f := range formats {
		fmt.Fprintf(os.Stderr, "  %s\n", f)
	}
}

// parseSince accepts a date, because an export is scoped by day in practice and asking for
// a full timestamp would be a worse trade.
func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("--since %q is not a date in YYYY-MM-DD form", s)
	}
	return t, nil
}

// reportPluginErrors prints per-plugin load failures. One broken plugin must not stop the
// working ones, and it must not pass unnoticed either.
func reportPluginErrors(errs []error) {
	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "baryo: ignoring plugin: %v\n", err)
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// displayPath shortens a path under the home directory, which is where these all live.
func displayPath(path string) string {
	if path == "" {
		return "(no home directory)"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
