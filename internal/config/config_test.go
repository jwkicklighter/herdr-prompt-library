package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFilesRecursivelyMergesProjectBeforeGlobalAndPreservesDuplicates(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "global")
	projectPath := filepath.Join(directory, "project")
	writePromptFile(t, globalPath, "shared.md", promptFile("shared", "Global shared prompt", "global contents"))
	writePromptFile(t, globalPath, "nested/global-only.md", promptFile("global-only", "Global only prompt", "global only"))
	writePromptFile(t, projectPath, "shared.md", promptFile("shared", "Project shared prompt", "project contents"))
	projectFile := writePromptFile(t, projectPath, "nested/project-only.md", promptFile("project-only", "Project only prompt", "project only"))

	prompts, err := LoadFiles(globalPath, projectPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ name, source string }{
		{"project-only", SourceProject},
		{"shared", SourceProject},
		{"global-only", SourceGlobal},
		{"shared", SourceGlobal},
	}
	if len(prompts) != len(want) {
		t.Fatalf("len(prompts) = %d, want %d", len(prompts), len(want))
	}
	for index, expected := range want {
		if prompts[index].Name != expected.name || prompts[index].Source != expected.source {
			t.Errorf("prompts[%d] = %#v, want name %q from %q", index, prompts[index], expected.name, expected.source)
		}
	}
	if prompts[0].Path != projectFile {
		t.Errorf("project prompt path = %q, want %q", prompts[0].Path, projectFile)
	}
}

func TestLoadFilesPreservesBodyExactly(t *testing.T) {
	for name, test := range map[string]struct {
		contents string
		body     string
	}{
		"LF":   {promptFile("exact", "Exact contents", "  keep leading whitespace\n\nkeep trailing spaces   \n"), "  keep leading whitespace\n\nkeep trailing spaces   \n"},
		"CRLF": {"---\r\ntitle: exact\r\ndescription: Exact contents\r\n---\r\n  keep\r\n", "  keep\r\n"},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			path := writePromptFile(t, directory, "prompts/exact.md", test.contents)

			prompts, err := LoadFiles("", filepath.Join(directory, "prompts"))
			if err != nil {
				t.Fatal(err)
			}
			if got := prompts[0].Contents; got != test.body {
				t.Errorf("contents = %q, want %q", got, test.body)
			}
			if prompts[0].Path != path {
				t.Errorf("path = %q, want %q", prompts[0].Path, path)
			}
		})
	}
}

func TestLoadFilesAllowsMissingDirectories(t *testing.T) {
	directory := t.TempDir()
	prompts, err := LoadFiles(filepath.Join(directory, "missing-global"), filepath.Join(directory, "missing-project"))
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 0 {
		t.Errorf("prompts = %#v, want no prompts", prompts)
	}
}

func TestLoadFilesRetainsValidPromptsWhenScopesHaveErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		badGlobal  bool
		wantSource string
	}{
		{name: "malformed global", badGlobal: true, wantSource: SourceProject},
		{name: "malformed project", wantSource: SourceGlobal},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			globalPath := filepath.Join(directory, "global")
			projectPath := filepath.Join(directory, "project")
			if test.badGlobal {
				writePromptFile(t, globalPath, "bad.md", "---\ntitle: missing closing delimiter\n")
				writePromptFile(t, projectPath, "valid.md", promptFile("project", "Project", "project body"))
			} else {
				writePromptFile(t, globalPath, "valid.md", promptFile("global", "Global", "global body"))
				writePromptFile(t, projectPath, "bad.md", "---\ntitle: missing closing delimiter\n")
			}

			prompts, err := LoadFiles(globalPath, projectPath)
			if err == nil || !strings.Contains(err.Error(), "bad.md") {
				t.Fatalf("error = %v, want path-specific malformed prompt error", err)
			}
			if len(prompts) != 1 || prompts[0].Source != test.wantSource {
				t.Errorf("prompts = %#v, want valid %s prompt", prompts, test.wantSource)
			}
		})
	}
}

func TestLoadFilesRetainsValidPromptsAlongsideMalformedFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "prompts")
	bad := writePromptFile(t, path, "bad.md", "not frontmatter")
	good := writePromptFile(t, path, "good.md", promptFile("good", "Good", "body"))

	prompts, err := LoadFiles("", path)
	if err == nil || !strings.Contains(err.Error(), bad) {
		t.Fatalf("error = %v, want path-specific malformed prompt error", err)
	}
	if len(prompts) != 1 || prompts[0].Path != good {
		t.Errorf("prompts = %#v, want valid prompt", prompts)
	}
}

func TestLoadFilesRejectsMalformedFrontmatterAndBlankFields(t *testing.T) {
	for name, test := range map[string]struct {
		contents string
		want     string
	}{
		"invalid YAML":      {"---\ntitle: [\ndescription: broken\n---\nbody", "frontmatter"},
		"blank title":       {promptFile(" \t", "description", "body"), "title must not be blank"},
		"blank description": {promptFile("title", " \t", "body"), "description must not be blank"},
		"blank body":        {promptFile("title", "description", " \t\n"), "contents must not be blank"},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			path := writePromptFile(t, directory, "prompts/prompt.md", test.contents)
			_, err := LoadFiles("", filepath.Join(directory, "prompts"))
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), test.want) {
				t.Errorf("error = %v, want %q and %q", err, path, test.want)
			}
		})
	}
}

func TestLoadUsesConfiguredGlobalAndProjectRoot(t *testing.T) {
	directory := t.TempDir()
	globalDirectory := filepath.Join(directory, "global")
	projectDirectory := filepath.Join(directory, "project")
	writePromptFile(t, filepath.Join(globalDirectory, "prompts"), "global.md", promptFile("global", "Global", "global"))
	writePromptFile(t, filepath.Join(projectDirectory, ".herdr", "prompts"), "project.md", promptFile("project", "Project", "project"))

	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", globalDirectory)
	t.Setenv("HERDR_PROMPT_LIBRARY_PROJECT_ROOT", projectDirectory)
	prompts, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 || prompts[0].Name != "project" || prompts[1].Name != "global" {
		t.Errorf("prompts = %#v, want project then global", prompts)
	}
}

func TestLoadUsesCurrentDirectoryWithoutProjectRoot(t *testing.T) {
	directory := t.TempDir()
	writePromptFile(t, filepath.Join(directory, ".herdr", "prompts"), "project.md", promptFile("project", "Project", "project"))

	t.Setenv("HERDR_PROMPT_LIBRARY_PROJECT_ROOT", "")
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

	prompts, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0].Name != "project" {
		t.Errorf("prompts = %#v, want current-directory prompt", prompts)
	}
}

func promptFile(title, description, body string) string {
	return "---\ntitle: " + title + "\ndescription: " + description + "\n---\n" + body
}

func writePromptFile(t *testing.T, directory, name, contents string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
