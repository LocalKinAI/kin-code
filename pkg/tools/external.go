package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/LocalKinAI/kincode/pkg/provider"
	"gopkg.in/yaml.v3"
)

// externalSkill is a tool whose implementation lives outside the
// kincode binary — typically a `node run.js` / `python run.py` /
// `bash run.sh` invocation declared in a SKILL.md file. Format
// matches the kinclaw kernel's external-skill convention exactly,
// so the same `~/.localkin/skills/<name>/` directory works for
// both kernels — install web_browser / image_gen / browser_session
// once, both kinclaw and kincode pick them up.
//
// SKILL.md frontmatter shape:
//
//	---
//	name: "web_browser"
//	description: "Browse a page via Playwright. Returns text + optional screenshot."
//	command: ["node", "/abs/path/to/run.js"]
//	args: ["--text"]                             # optional fixed args
//	timeout: 30                                  # seconds, default 30
//	schema:                                      # optional param schema
//	  url:
//	    type: "string"
//	    description: "URL to fetch"
//	    required: true
//	  selector:
//	    type: "string"
//	    description: "Optional CSS selector to extract"
//	---
//	# web_browser
//	[free-form markdown body, ignored at exec time]
//
// Templates: `{{name}}` placeholders in command/args get substituted
// from Execute's args map. Unsubstituted placeholders (caller omitted
// the param) get stripped to "" so SKILL.md authors can detect
// "missing" with `[ -n "$X" ]` instead of brittle sentinel checks.
//
// Execution: subprocess inherits SafeEnv plus SKILL_DIR=<absolute
// path of the skill dir>. cwd is set to the skill dir so relative
// `python "$SKILL_DIR/run.py"` patterns work.
type externalSkill struct {
	meta skillMeta
	dir  string
	def  provider.ToolDef
}

type skillMeta struct {
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description"`
	Command     []string               `yaml:"command"`
	Args        []string               `yaml:"args"`
	Schema      map[string]skillParam  `yaml:"schema"`
	Timeout     int                    `yaml:"timeout"`
}

type skillParam struct {
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

// unsubstitutedTemplate matches any leftover `{{name}}` placeholder
// after the named substitution pass — see comment above.
var unsubstitutedTemplate = regexp.MustCompile(`\{\{[A-Za-z_][A-Za-z0-9_]*\}\}`)

// LoadExternalSkill parses a single SKILL.md file. The dir containing
// it becomes the subprocess cwd + SKILL_DIR.
func LoadExternalSkill(path string) (Tool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rawYAML, _, err := splitFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("parsing SKILL.md %s: %w", path, err)
	}
	var meta skillMeta
	if err := yaml.Unmarshal(rawYAML, &meta); err != nil {
		return nil, fmt.Errorf("parsing SKILL.md YAML: %w", err)
	}
	if meta.Name == "" || meta.Description == "" || len(meta.Command) == 0 {
		return nil, fmt.Errorf("SKILL.md must have name, description, and command")
	}
	if meta.Timeout <= 0 {
		meta.Timeout = 30
	}

	// Build the JSON-Schema-shaped parameters for the provider tool def.
	props := map[string]any{}
	var required []string
	for k, v := range meta.Schema {
		typ := v.Type
		if typ == "" {
			typ = "string"
		}
		props[k] = map[string]any{
			"type":        typ,
			"description": v.Description,
		}
		if v.Required {
			required = append(required, k)
		}
	}
	parameters := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		parameters["required"] = required
	}

	return &externalSkill{
		meta: meta,
		dir:  filepath.Dir(path),
		def:  provider.NewToolDef(meta.Name, meta.Description, parameters),
	}, nil
}

func (s *externalSkill) Name() string             { return s.meta.Name }
func (s *externalSkill) Description() string      { return s.meta.Description }
func (s *externalSkill) Def() provider.ToolDef    { return s.def }

func (s *externalSkill) Execute(args map[string]any) (string, error) {
	// Coerce args to a flat map[string]string for substitution.
	// Numbers, bools, etc. stringify via fmt.Sprint — same approach
	// the kincode server uses for tool_call event Params encoding.
	flat := make(map[string]string, len(args))
	for k, v := range args {
		switch s := v.(type) {
		case string:
			flat[k] = s
		case nil:
			flat[k] = ""
		default:
			flat[k] = fmt.Sprint(s)
		}
	}

	subst := func(s string) string {
		for k, v := range flat {
			s = strings.ReplaceAll(s, "{{"+k+"}}", v)
		}
		// Strip unsubstituted placeholders so authors can use
		// `[ -n "$X" ]` patterns to detect absence.
		return unsubstitutedTemplate.ReplaceAllString(s, "")
	}

	cmdParts := make([]string, len(s.meta.Command))
	for i, a := range s.meta.Command {
		cmdParts[i] = subst(a)
	}
	extraArgs := make([]string, len(s.meta.Args))
	for i, a := range s.meta.Args {
		extraArgs[i] = subst(a)
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(s.meta.Timeout)*time.Second)
	defer cancel()

	cmdArgv := append(cmdParts[1:], extraArgs...)
	cmd := exec.CommandContext(ctx, cmdParts[0], cmdArgv...)
	cmd.Dir = s.dir

	// Resolve SKILL_DIR to absolute. Without this, a SKILL.md that
	// does `python3 "$SKILL_DIR/web.py"` ends up double-prefixing
	// because cwd is ALSO the relative dir.
	absDir := s.dir
	if a, err := filepath.Abs(s.dir); err == nil {
		absDir = a
	}
	cmd.Env = append(os.Environ(), "SKILL_DIR="+absDir)

	out, err := cmd.CombinedOutput()
	const maxOutput = 128 * 1024
	result := string(out)
	if len(result) > maxOutput {
		result = result[:maxOutput] + "\n... (truncated)"
	}
	if err != nil {
		// Non-zero exit is folded into the result so the agent sees
		// the same string the user does — same convention as bash.
		return result + "\nError: " + err.Error(), nil
	}
	return result, nil
}

// LoadExternalSkillsFromDir walks `dir/<name>/SKILL.md` and returns
// every successfully-parsed skill. Missing dir is not an error
// (returns empty list); broken SKILL.md files log a warning and
// get skipped so one bad skill doesn't break the rest.
func LoadExternalSkillsFromDir(dir string) []Tool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var skills []Tool
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(dir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			continue
		}
		s, err := LoadExternalSkill(skillPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[skill] skipping %s: %v\n",
				skillPath, err)
			continue
		}
		skills = append(skills, s)
	}
	return skills
}

// LoadAllExternalSkills loads from the standard search paths in
// priority order:
//
//   1. ~/.kincode/skills/         — kincode-specific skills
//   2. ~/.localkin/skills/         — shared LocalKin family skills
//                                   (kinclaw + kincode use the same
//                                   web_browser, image_gen, etc.)
//
// Earlier paths win on name conflicts: a kincode-specific override
// takes precedence over the shared family skill of the same name.
// Returns the merged list ready for Registry.Register.
func LoadAllExternalSkills() []Tool {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := []string{
		filepath.Join(home, ".kincode", "skills"),
		filepath.Join(home, ".localkin", "skills"),
	}
	seen := map[string]bool{}
	var merged []Tool
	for _, p := range paths {
		for _, s := range LoadExternalSkillsFromDir(p) {
			if seen[s.Name()] {
				continue
			}
			seen[s.Name()] = true
			merged = append(merged, s)
		}
	}
	return merged
}

// splitFrontmatter splits a `---\nYAML\n---\nbody` document into
// (yamlBytes, body). Returns error if the doc doesn't start with
// `---` or has no closing `---`. Mirrors kinclaw's soul.SplitFrontmatter
// without the import dependency.
func splitFrontmatter(data []byte) ([]byte, []byte, error) {
	s := string(data)
	if !strings.HasPrefix(s, "---") {
		return nil, nil, fmt.Errorf("missing leading ---")
	}
	rest := s[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, nil, fmt.Errorf("missing closing ---")
	}
	yamlPart := rest[:idx]
	bodyStart := idx + 4 // past "\n---"
	if bodyStart < len(rest) && rest[bodyStart] == '\n' {
		bodyStart++
	}
	body := ""
	if bodyStart <= len(rest) {
		body = rest[bodyStart:]
	}
	return []byte(yamlPart), []byte(body), nil
}
