package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFilesMergesProjectBeforeGlobalAndPreservesDuplicates(t *testing.T) {
	directory := t.TempDir()
	globalPath := writePromptFile(t, directory, "global.toml", `
[[prompts]]
name = "shared"
description = "Global shared prompt"
contents = "global contents"

[[prompts]]
name = "global-only"
description = "Global only prompt"
contents = "global only"
`)
	projectPath := writePromptFile(t, directory, "project.toml", `
[[prompts]]
name = "shared"
description = "Project shared prompt"
contents = "project contents"

[[prompts]]
name = "project-only"
description = "Project only prompt"
contents = "project only"
`)

	prompts, err := LoadFiles(globalPath, projectPath)
	if err != nil {
		t.Fatal(err)
	}

	if len(prompts) != 4 {
		t.Fatalf("len(prompts) = %d, want 4", len(prompts))
	}
	want := []struct{ name, source string }{
		{"shared", SourceProject},
		{"project-only", SourceProject},
		{"shared", SourceGlobal},
		{"global-only", SourceGlobal},
	}
	for index, expected := range want {
		if prompts[index].Name != expected.name || prompts[index].Source != expected.source {
			t.Errorf("prompts[%d] = %#v, want name %q from %q", index, prompts[index], expected.name, expected.source)
		}
	}
}

func TestLoadFilesPreservesMultilineContentsExactly(t *testing.T) {
	directory := t.TempDir()
	contents := "  keep leading whitespace\n\nkeep trailing spaces   \n"
	path := writePromptFile(t, directory, "prompts.toml", "[[prompts]]\nname = \"exact\"\ndescription = \"Exact contents\"\ncontents = '''"+contents+"'''\n")

	prompts, err := LoadFiles("", path)
	if err != nil {
		t.Fatal(err)
	}
	if got := prompts[0].Contents; got != contents {
		t.Errorf("contents = %q, want %q", got, contents)
	}
}

func TestLoadFilesAllowsMissingScopes(t *testing.T) {
	directory := t.TempDir()
	prompts, err := LoadFiles(filepath.Join(directory, "missing-global.toml"), filepath.Join(directory, "missing-project.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 0 {
		t.Errorf("prompts = %#v, want no prompts", prompts)
	}
}

func TestLoadFilesSupportsEachScopeIndependently(t *testing.T) {
	directory := t.TempDir()
	globalPath := writePromptFile(t, directory, "global.toml", "[[prompts]]\nname = \"global\"\ndescription = \"Global\"\ncontents = \"global\"\n")
	projectPath := writePromptFile(t, directory, "project.toml", "[[prompts]]\nname = \"project\"\ndescription = \"Project\"\ncontents = \"project\"\n")

	for _, test := range []struct {
		name        string
		globalPath  string
		projectPath string
		wantName    string
		wantSource  string
	}{
		{name: "global only", globalPath: globalPath, projectPath: filepath.Join(directory, "missing-project.toml"), wantName: "global", wantSource: SourceGlobal},
		{name: "project only", globalPath: filepath.Join(directory, "missing-global.toml"), projectPath: projectPath, wantName: "project", wantSource: SourceProject},
	} {
		t.Run(test.name, func(t *testing.T) {
			prompts, err := LoadFiles(test.globalPath, test.projectPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(prompts) != 1 || prompts[0].Name != test.wantName || prompts[0].Source != test.wantSource {
				t.Errorf("prompts = %#v, want %q from %q", prompts, test.wantName, test.wantSource)
			}
		})
	}
}

func TestLoadFilesReportsMalformedFilePathAndLocation(t *testing.T) {
	directory := t.TempDir()
	path := writePromptFile(t, directory, "bad.toml", "[[prompts]]\nname = \"missing quote\n")

	_, err := LoadFiles("", path)
	if err == nil {
		t.Fatal("LoadFiles unexpectedly succeeded")
	}
	message := err.Error()
	for _, value := range []string{path, "project", "line"} {
		if !strings.Contains(message, value) {
			t.Errorf("error %q does not contain %q", message, value)
		}
	}
}

func TestLoadFilesRejectsBlankRequiredFields(t *testing.T) {
	for _, field := range []string{"name", "description", "contents"} {
		t.Run(field, func(t *testing.T) {
			directory := t.TempDir()
			path := writePromptFile(t, directory, "prompts.toml", "[[prompts]]\nname = \"name\"\ndescription = \"description\"\ncontents = \"contents\"\n")
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(strings.Replace(string(contents), field+" = \""+field+"\"", field+" = \" \\t\"", 1)), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err = LoadFiles("", path)
			if err == nil || !strings.Contains(err.Error(), field+" must not be blank") || !strings.Contains(err.Error(), path) {
				t.Errorf("error = %v, want actionable validation error for %q in %q", err, field, path)
			}
		})
	}
}

func TestLoadUsesConfiguredGlobalAndCurrentProject(t *testing.T) {
	directory := t.TempDir()
	globalDirectory := filepath.Join(directory, "global")
	projectDirectory := filepath.Join(directory, "project")
	if err := os.MkdirAll(filepath.Join(projectDirectory, ".herdr"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePromptFile(t, globalDirectory, "prompts.toml", "[[prompts]]\nname = \"global\"\ndescription = \"Global\"\ncontents = \"global\"\n")
	writePromptFile(t, filepath.Join(projectDirectory, ".herdr"), "prompts.toml", "[[prompts]]\nname = \"project\"\ndescription = \"Project\"\ncontents = \"project\"\n")

	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", globalDirectory)
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(projectDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

	prompts, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 || prompts[0].Name != "project" || prompts[1].Name != "global" {
		t.Errorf("prompts = %#v, want project then global", prompts)
	}
}

func TestLoadUsesProjectRootEnvironment(t *testing.T) {
	directory := t.TempDir()
	projectDirectory := filepath.Join(directory, "project")
	otherDirectory := filepath.Join(directory, "other")
	if err := os.MkdirAll(filepath.Join(projectDirectory, ".herdr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	writePromptFile(t, filepath.Join(projectDirectory, ".herdr"), "prompts.toml", "[[prompts]]\nname = \"project\"\ndescription = \"Project\"\ncontents = \"project\"\n")

	t.Setenv("HERDR_PROMPT_LIBRARY_PROJECT_ROOT", projectDirectory)
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(otherDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

	prompts, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0].Name != "project" {
		t.Errorf("prompts = %#v, want project-root prompt", prompts)
	}
}

func writePromptFile(t *testing.T, directory, name, contents string) string {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
