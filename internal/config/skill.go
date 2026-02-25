// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill represents a loaded skill from a SKILL.md file.
type Skill struct {
	Name         string            // from frontmatter
	Description  string            // from frontmatter
	TriggerWords []string          // keywords extracted from description + body
	Content      string            // markdown body after frontmatter (empty until activated)
	Scripts      []string          // filenames in scripts/ directory (empty until activated)
	Resources    map[string]string // resource file contents keyed by relative path (empty until activated)
	RequiresFile string            // path to requirements.txt if found (empty if none)
	Dir          string            // path to the skill directory
}

// skillFrontmatter is the YAML frontmatter parsed from SKILL.md.
type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// SkillIndex scans standard skill directories and returns a lightweight index
// containing only name, description, trigger words, and directory path.
// Full content is NOT loaded — use LoadSkill() to activate a specific skill.
//
// Directories scanned:
//
//   - ~/.baryo/skills/*/SKILL.md  (global)
//   - .baryo/skills/*/SKILL.md    (project config dir)
//   - skills/*/SKILL.md           (project root)
func SkillIndex() []Skill {
	var skills []Skill
	seen := make(map[string]bool)

	home, _ := os.UserHomeDir()

	var dirs []string
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".baryo", "skills"))
	}
	dirs = append(dirs,
		filepath.Join(".baryo", "skills"),
		"skills",
	)

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillDir := filepath.Join(dir, e.Name())
			skillFile := filepath.Join(skillDir, "SKILL.md")

			data, err := os.ReadFile(skillFile)
			if err != nil {
				continue
			}

			fm, body, err := parseFrontmatter(string(data))
			if err != nil {
				continue
			}

			name := fm.Name
			if name == "" {
				name = e.Name()
			}

			// Skip template skill
			if name == "template-skill" || name == "_template" {
				continue
			}

			triggers := extractTriggerWords(fm.Description, body)

			skill := Skill{
				Name:         name,
				Description:  fm.Description,
				TriggerWords: triggers,
				Dir:          skillDir,
			}

			// Deduplicate: later paths (project) override earlier (global)
			if seen[name] {
				for i, s := range skills {
					if s.Name == name {
						skills[i] = skill
						break
					}
				}
			} else {
				seen[name] = true
				skills = append(skills, skill)
			}
		}
	}

	return skills
}

// extractTriggerWords builds a list of trigger keywords from the skill's
// description and body content. These are used for auto-activation matching.
func extractTriggerWords(description, body string) []string {
	seen := make(map[string]bool)
	var words []string

	add := func(w string) {
		w = strings.ToLower(strings.TrimSpace(w))
		if w != "" && !seen[w] {
			seen[w] = true
			words = append(words, w)
		}
	}

	// 1. Extract file extensions from description (.pdf, .docx, etc.)
	// Only from the positive part (before "Do NOT")
	posDesc := description
	if i := strings.Index(strings.ToLower(posDesc), "do not"); i != -1 {
		posDesc = posDesc[:i]
	}
	extRe := regexp.MustCompile(`\.([a-z]{2,5})\b`)
	for _, m := range extRe.FindAllString(posDesc, -1) {
		add(m)                // .pdf
		add(m[1:])            // pdf
		add(m[1:] + " file") // pdf file
	}

	// 2. Extract explicit Keywords section from body (only the keyword line itself)
	for _, line := range strings.Split(body, "\n") {
		trimLine := strings.TrimSpace(line)
		// Match lines like "## Keywords", "**Keywords**:", "Keywords:"
		stripped := strings.TrimLeft(trimLine, "#*_ ")
		strippedLower := strings.ToLower(stripped)
		if !strings.HasPrefix(strippedLower, "keyword") {
			continue
		}
		// Strip the "Keywords:" prefix
		if i := strings.Index(strippedLower, ":"); i != -1 {
			stripped = strings.TrimSpace(stripped[i+1:])
		} else {
			continue // "Keywords" without ":" — skip (it's a heading, content on next line)
		}
		trimLine = stripped
		if trimLine == "" {
			continue
		}
		for _, kw := range strings.Split(trimLine, ",") {
			kw = strings.TrimSpace(kw)
			if len(kw) >= 2 && len(kw) <= 40 {
				add(kw)
			}
		}
	}

	// 3. Extract quoted trigger phrases from description (skip negations)
	quoteRe := regexp.MustCompile(`['"]([^'"]+)['"]`)
	// Find "Do NOT" boundaries so we skip quoted terms in negation clauses
	negIdx := -1
	descLowerForNeg := strings.ToLower(description)
	if i := strings.Index(descLowerForNeg, "do not"); i != -1 {
		negIdx = i
	}
	for _, m := range quoteRe.FindAllStringSubmatchIndex(description, -1) {
		start, end := m[2], m[3]
		phrase := strings.ToLower(description[start:end])
		// Skip if in a "Do NOT" clause
		if negIdx != -1 && start > negIdx {
			continue
		}
		// Clean up trailing punctuation
		phrase = strings.TrimRight(phrase, ".,;:")
		if len(phrase) >= 3 && len(phrase) <= 40 {
			add(phrase)
		}
	}

	// 4. Key noun phrases from description (common patterns)
	// Only match in the positive part of the description (before "Do NOT")
	positiveDesc := strings.ToLower(description)
	if i := strings.Index(positiveDesc, "do not"); i != -1 {
		positiveDesc = positiveDesc[:i]
	}
	triggerPatterns := []string{
		"incident report", "status report", "leadership update",
		"3p update", "company newsletter", "faq",
		"word doc", "word document",
		"slide deck", "pitch deck", "presentation", "slides",
		"spreadsheet", "excel",
		"pdf", "pdf form", "pdf file",
		"generative art", "algorithmic art", "flow field", "particle system",
		"brand color", "brand guideline", "visual identity",
		"landing page", "dashboard", "react component", "web component",
		"mcp server", "model context protocol",
		"animated gif", "gif for slack",
		"documentation", "technical spec", "proposal", "decision doc",
		"webapp test", "playwright", "browser screenshot",
		"create a skill", "skill performance", "run eval",
		"theme", "styling artifact",
	}
	for _, p := range triggerPatterns {
		if strings.Contains(positiveDesc, p) {
			add(p)
		}
	}

	return words
}

// MatchSkill finds the best matching skill for user input.
// Returns the skill name and match score (0 = no match).
// A skill must have at least 1 keyword match to be considered.
func MatchSkill(input string, index []Skill, activeSkills map[string]bool) (string, int) {
	lower := strings.ToLower(input)
	bestName := ""
	bestScore := 0

	for _, s := range index {
		// Skip already-activated skills
		if _, active := activeSkills[s.Name]; active {
			continue
		}

		score := 0
		for _, kw := range s.TriggerWords {
			if strings.Contains(lower, kw) {
				// Longer keyword matches are worth more
				score += len(kw)
			}
		}

		// Bonus when the skill name itself appears in the input.
		// This helps format-named skills (pdf, docx, xlsx) beat skills
		// that merely mention those formats as output types.
		nameLower := strings.ToLower(s.Name)
		if len(nameLower) >= 3 && strings.Contains(lower, nameLower) {
			score += len(nameLower)
		}

		if score > bestScore {
			bestScore = score
			bestName = s.Name
		}
	}

	return bestName, bestScore
}

// LoadSkill fully loads a skill by name from the index. Returns the skill
// with Content and Scripts populated, or an error if not found.
func LoadSkill(name string, index []Skill) (Skill, error) {
	for _, s := range index {
		if strings.EqualFold(s.Name, name) {
			return loadFullSkill(s.Dir)
		}
	}
	return Skill{}, fmt.Errorf("skill %q not found", name)
}

// maxResourceFileSize is the max size (bytes) for a resource file to be inlined.
const maxResourceFileSize = 8192

// maxTotalResources is the max total bytes of resource content to inline.
const maxTotalResources = 32000

// scriptDirs are directories scanned for executable scripts within a skill.
var scriptDirs = []string{"scripts", "core", "eval-viewer"}

// resourceDirs are directories scanned for resource/example files within a skill.
var resourceDirs = []string{"examples", "resources", "assets"}

// scriptExts are file extensions recognized as scripts.
var scriptExts = map[string]bool{
	".py": true, ".sh": true, ".js": true, ".ts": true, ".rb": true,
}

// resourceExts are file extensions that are inlined as text content.
var resourceExts = map[string]bool{
	".md": true, ".txt": true, ".json": true, ".yaml": true, ".yml": true,
	".xml": true, ".html": true, ".csv": true, ".toml": true,
	".py": true, ".sh": true, ".js": true, // scripts in resource dirs are also useful context
}

// loadFullSkill reads the full SKILL.md content, discovers scripts,
// and loads resource files.
func loadFullSkill(dir string) (Skill, error) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Skill{}, err
	}

	fm, body, err := parseFrontmatter(string(data))
	if err != nil {
		return Skill{}, err
	}

	name := fm.Name
	if name == "" {
		name = filepath.Base(dir)
	}

	// Discover scripts from all known script directories
	scripts := discoverScripts(dir)

	// Load resource files
	resources := loadResources(dir)

	// Find requirements.txt (check skill root and scripts/)
	reqFile := findRequirements(dir)

	return Skill{
		Name:         name,
		Description:  fm.Description,
		Content:      body,
		Scripts:      scripts,
		Resources:    resources,
		RequiresFile: reqFile,
		Dir:          dir,
	}, nil
}

// findRequirements looks for requirements.txt in a skill directory.
func findRequirements(dir string) string {
	candidates := []string{
		filepath.Join(dir, "requirements.txt"),
		filepath.Join(dir, "scripts", "requirements.txt"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// discoverScripts scans script directories recursively for executable files.
func discoverScripts(dir string) []string {
	var scripts []string

	for _, subdir := range scriptDirs {
		sDir := filepath.Join(dir, subdir)
		if _, err := os.Stat(sDir); err != nil {
			continue
		}
		filepath.Walk(sDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(info.Name()))
			if scriptExts[ext] {
				rel, _ := filepath.Rel(dir, path)
				scripts = append(scripts, rel)
			}
			return nil
		})
	}

	return scripts
}

// loadResources scans resource directories and loads text file contents.
func loadResources(dir string) map[string]string {
	resources := make(map[string]string)
	totalSize := 0

	for _, subdir := range resourceDirs {
		resDir := filepath.Join(dir, subdir)
		entries, err := os.ReadDir(resDir)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}

			ext := strings.ToLower(filepath.Ext(e.Name()))
			if !resourceExts[ext] {
				continue
			}

			info, err := e.Info()
			if err != nil || info.Size() > maxResourceFileSize {
				continue
			}

			if totalSize+int(info.Size()) > maxTotalResources {
				break
			}

			filePath := filepath.Join(resDir, e.Name())
			content, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}

			relPath := filepath.Join(subdir, e.Name())
			resources[relPath] = string(content)
			totalSize += len(content)
		}
	}

	return resources
}

// parseFrontmatter extracts YAML frontmatter from markdown content.
// Returns the parsed frontmatter and the remaining body.
func parseFrontmatter(content string) (skillFrontmatter, string, error) {
	var fm skillFrontmatter

	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		// No frontmatter — treat entire content as body
		return fm, trimmed, nil
	}

	// Find closing ---
	rest := trimmed[3:] // skip opening ---
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		return fm, trimmed, fmt.Errorf("unclosed frontmatter")
	}

	yamlBlock := rest[:idx]
	body := strings.TrimSpace(rest[idx+4:]) // skip \n---

	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return fm, body, fmt.Errorf("invalid frontmatter YAML: %w", err)
	}

	return fm, body, nil
}

// FormatSkillIndex formats the skill index as a compact system prompt section.
// Only includes names and short descriptions — not full content.
func FormatSkillIndex(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Available Skills\n\n")
	b.WriteString("Skills are auto-activated when the user's message matches. The user can also use `/skill <name>` manually.\n\n")

	for _, s := range skills {
		desc := s.Description
		// Keep index compact — truncate long descriptions
		if len(desc) > 150 {
			desc = desc[:150] + "..."
		}
		b.WriteString(fmt.Sprintf("- **%s** — %s\n", s.Name, desc))
	}

	return b.String()
}

// FormatActivatedSkill formats a fully loaded skill for injection into context.
// Includes full SKILL.md content, script listing, resource file contents,
// and execution instructions so the model knows how to run code.
func FormatActivatedSkill(s Skill) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Skill: %s\n\n", s.Name))
	if s.Description != "" {
		b.WriteString(s.Description + "\n\n")
	}

	// Execution instructions only for skills that have scripts (code-oriented).
	// Knowledge-only skills (doc-coauthoring, brand-guidelines, etc.) skip this.
	if len(s.Scripts) > 0 {
		cwd, _ := os.Getwd()
		outDir := filepath.Join(cwd, "output_files")
		lang := detectPrimaryLanguage(s.Scripts)
		b.WriteString("## IMPORTANT: How to Execute Code\n\n")
		b.WriteString("When the user asks you to create/generate/build something:\n")
		b.WriteString("1. Write the code yourself\n")
		b.WriteString("2. Call `run_code` IMMEDIATELY to execute it\n")
		fmt.Fprintf(&b, "3. Save output files to `%s/` using absolute paths\n", outDir)
		b.WriteString("4. Report the result and include the file path as a link (e.g. file://path/to/output.gif)\n\n")
		b.WriteString("Do NOT just describe what you would create. Do NOT ask for confirmation. EXECUTE CODE NOW.\n\n")
		fmt.Fprintf(&b, "Use EXACTLY this format:\n<tool_call>{\"name\": \"run_code\", \"arguments\": {\"code\": \"import os\\nos.makedirs('%s', exist_ok=True)\\n# ... your code ...\\n# ALWAYS use absolute path to save:\\n# output_path = '%s/filename.ext'\", \"language\": \"%s\", \"working_dir\": \"%s\"}}</tool_call>\n\n", outDir, outDir, lang, s.Dir)
		b.WriteString("Rules: Use `run_code` (argument is \"code\") for code you write. Use `run_script` only for existing files.\n\n")

		b.WriteString("**Available scripts** (use `run_script` tool):\n")
		for _, script := range s.Scripts {
			scriptPath := filepath.Join(s.Dir, script)
			fmt.Fprintf(&b, "- `%s`\n", scriptPath)
		}
		b.WriteString("\n")
	}

	// Skill content after instructions
	if s.Content != "" {
		b.WriteString("## Skill Reference\n\n")
		b.WriteString(s.Content + "\n\n")
	}
	if len(s.Resources) > 0 {
		b.WriteString("## Resource Files\n\n")
		b.WriteString("These are the actual files available in this skill. Use their EXACT names when referencing them.\n\n")
		// Sort keys for deterministic output
		keys := make([]string, 0, len(s.Resources))
		for k := range s.Resources {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, path := range keys {
			content := s.Resources[path]
			fmt.Fprintf(&b, "### File: `%s`\n\n", path)
			b.WriteString(content)
			b.WriteString("\n\n---\n\n")
		}
	}
	return b.String()
}

// detectPrimaryLanguage counts script file extensions and returns the dominant language.
// Falls back to "python" if no scripts or ambiguous.
func detectPrimaryLanguage(scripts []string) string {
	counts := map[string]int{}
	for _, s := range scripts {
		ext := strings.ToLower(filepath.Ext(s))
		switch ext {
		case ".py":
			counts["python"]++
		case ".js", ".ts":
			counts["node"]++
		case ".sh":
			counts["shell"]++
		}
	}
	best := "python"
	bestCount := 0
	for lang, count := range counts {
		if count > bestCount {
			best = lang
			bestCount = count
		}
	}
	return best
}

// sortStrings sorts a string slice in place (simple insertion sort to avoid importing sort).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
