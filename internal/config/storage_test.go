package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLibrariesCreateBothScopesWithEscapedFrontmatterAndExactBody(t *testing.T) {
	libraries := testLibraries(t)
	for _, source := range []string{SourceProject, SourceGlobal} {
		t.Run(source, func(t *testing.T) {
			body := "  preserve whitespace\n$HOME\n"
			created, err := libraries.Create(source, Prompt{Name: "Review: \"quoted\"", Description: "line one\nline two: value", Contents: body})
			if err != nil {
				t.Fatal(err)
			}
			if created.Source != source || filepath.Base(created.Path) != "review-quoted.md" {
				t.Errorf("created = %#v", created)
			}
			loaded, err := loadPrompt(created.Path, source)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Name != created.Name || loaded.Description != created.Description || loaded.Contents != body {
				t.Errorf("loaded = %#v, want %#v", loaded, created)
			}
		})
	}
}

func TestLibrariesCreateUsesDeterministicCollisionSuffixesWithoutOverwrite(t *testing.T) {
	libraries := testLibraries(t)
	first, err := libraries.Create(SourceProject, Prompt{Name: "Same Prompt", Description: "first", Contents: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := libraries.Create(SourceProject, Prompt{Name: "Same Prompt", Description: "second", Contents: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first.Path) != "same-prompt.md" || filepath.Base(second.Path) != "same-prompt-2.md" {
		t.Errorf("paths = %q, %q", first.Path, second.Path)
	}
	contents, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "first") || strings.Contains(string(contents), "second") {
		t.Errorf("first prompt was overwritten: %q", contents)
	}
}

func TestLibrariesUpdatePreservesFilenameUnknownMetadataAndBody(t *testing.T) {
	libraries := testLibraries(t)
	path := writePromptFile(t, libraries.LocalDir, "folder/original.md", "---\ntitle: Original\ndescription: Before\ncustom: [one, two]\n---\nold body\n")
	prompt := Prompt{Name: "Original", Description: "Before", Contents: "old body\n", Source: SourceProject, Path: path}
	updated, err := libraries.Update(prompt, Prompt{Name: "Changed: title", Description: "Changed\nDescription", Contents: "  exact new body\n"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Path != path || filepath.Base(updated.Path) != "original.md" || updated.Contents != "  exact new body\n" {
		t.Errorf("updated = %#v", updated)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "custom: [one, two]") {
		t.Errorf("unknown metadata lost: %q", contents)
	}
	loaded, err := loadPrompt(path, SourceProject)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != updated.Name || loaded.Description != updated.Description || loaded.Contents != updated.Contents {
		t.Errorf("loaded = %#v, want %#v", loaded, updated)
	}
}

func TestLibrariesUpdateFailureLeavesFileIntact(t *testing.T) {
	libraries := testLibraries(t)
	prompt := createTestPrompt(t, libraries, SourceProject, "Original")
	before, err := os.ReadFile(prompt.Path)
	if err != nil {
		t.Fatal(err)
	}
	restore := replaceRename(t, func(_, _ string) error { return errors.New("rename failed") })
	defer restore()
	if _, err := libraries.Update(prompt, Prompt{Name: "Changed", Description: "Changed", Contents: "changed"}); err == nil {
		t.Fatal("Update unexpectedly succeeded")
	}
	after, err := os.ReadFile(prompt.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("failed update modified prompt: %q", after)
	}
}

func TestLibrariesDeleteRejectsOutsideAndSymlinkEscapes(t *testing.T) {
	libraries := testLibraries(t)
	outside := writePromptFile(t, t.TempDir(), "outside.md", promptFile("outside", "Outside", "body"))
	for name, prompt := range map[string]Prompt{
		"outside": {Source: SourceProject, Path: outside},
		"symlink": func() Prompt {
			link := filepath.Join(libraries.LocalDir, "escape.md")
			if err := os.MkdirAll(libraries.LocalDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			return Prompt{Source: SourceProject, Path: link}
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := libraries.Delete(prompt); err == nil || !strings.Contains(err.Error(), "outside") {
				t.Errorf("Delete error = %v, want outside-library rejection", err)
			}
			if _, err := libraries.Update(prompt, Prompt{Name: "Changed", Description: "Changed", Contents: "changed"}); err == nil || !strings.Contains(err.Error(), "outside") {
				t.Errorf("Update error = %v, want outside-library rejection", err)
			}
		})
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("outside prompt was modified: %v", err)
	}
}

func TestLibrariesDuplicateAndMovePreserveRawFileAcrossScopes(t *testing.T) {
	libraries := testLibraries(t)
	raw := "---\ntitle: Original\ndescription: Description\ncustom: true\n---\n  raw body\n"
	path := writePromptFile(t, libraries.LocalDir, "original.md", raw)
	prompt := Prompt{Name: "Original", Description: "Description", Contents: "  raw body\n", Source: SourceProject, Path: path}

	duplicate, err := libraries.Duplicate(prompt, SourceGlobal)
	if err != nil {
		t.Fatal(err)
	}
	duplicateContents, err := os.ReadFile(duplicate.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(duplicateContents) != raw || duplicate.Source != SourceGlobal {
		t.Errorf("duplicate contents = %q, source = %q", duplicateContents, duplicate.Source)
	}

	moved, err := libraries.Move(duplicate, SourceProject)
	if err != nil {
		t.Fatal(err)
	}
	movedContents, err := os.ReadFile(moved.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(movedContents) != raw || moved.Source != SourceProject || filepath.Base(moved.Path) != "original-2.md" {
		t.Errorf("moved = %#v, contents = %q", moved, movedContents)
	}
	if _, err := os.Stat(duplicate.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("duplicate source remains after move: %v", err)
	}

	movedAgain, err := libraries.Move(prompt, SourceGlobal)
	if err != nil {
		t.Fatal(err)
	}
	movedAgainContents, err := os.ReadFile(movedAgain.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(movedAgainContents) != raw || movedAgain.Source != SourceGlobal || filepath.Base(movedAgain.Path) != "original.md" {
		t.Errorf("second move = %#v, contents = %q", movedAgain, movedAgainContents)
	}
	if _, err := os.Stat(prompt.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("local source remains after move: %v", err)
	}
}

func TestLibrariesDuplicateAllowsSameScope(t *testing.T) {
	libraries := testLibraries(t)
	prompt := createTestPrompt(t, libraries, SourceGlobal, "Copy me")
	duplicate, err := libraries.Duplicate(prompt, SourceGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Source != SourceGlobal || filepath.Base(duplicate.Path) != "copy-me-2.md" {
		t.Errorf("duplicate = %#v", duplicate)
	}
}

func TestLibrariesMoveReportsPartialFailure(t *testing.T) {
	libraries := testLibraries(t)
	prompt := createTestPrompt(t, libraries, SourceProject, "Move me")
	restore := replaceRemove(t, func(path string) error {
		if path == prompt.Path {
			return errors.New("source removal failed")
		}
		return os.Remove(path)
	})
	defer restore()

	moved, err := libraries.Move(prompt, SourceGlobal)
	var partial *PartialMoveError
	if !errors.As(err, &partial) {
		t.Fatalf("Move error = %v, want PartialMoveError", err)
	}
	if partial.DestinationPath != moved.Path || moved.Source != SourceGlobal {
		t.Errorf("partial = %#v, moved = %#v", partial, moved)
	}
	if _, err := os.Stat(prompt.Path); err != nil {
		t.Errorf("source should remain after partial move: %v", err)
	}
	if _, err := os.Stat(moved.Path); err != nil {
		t.Errorf("destination should exist after partial move: %v", err)
	}
}

func TestLibrariesCreateFailureDoesNotLeaveFiles(t *testing.T) {
	libraries := testLibraries(t)
	restore := replaceCreateTemp(t, func(string, string) (*os.File, error) { return nil, errors.New("permission denied") })
	defer restore()
	if _, err := libraries.Create(SourceGlobal, Prompt{Name: "Nope", Description: "Nope", Contents: "body"}); err == nil {
		t.Fatal("Create unexpectedly succeeded")
	}
	entries, err := os.ReadDir(libraries.GlobalDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("failed create left entries: %#v", entries)
	}
}

func testLibraries(t *testing.T) Libraries {
	t.Helper()
	directory := t.TempDir()
	return Libraries{LocalDir: filepath.Join(directory, "local", "prompts"), GlobalDir: filepath.Join(directory, "global", "prompts")}
}

func createTestPrompt(t *testing.T, libraries Libraries, source, title string) Prompt {
	t.Helper()
	prompt, err := libraries.Create(source, Prompt{Name: title, Description: "Description", Contents: "body"})
	if err != nil {
		t.Fatal(err)
	}
	return prompt
}

func replaceRename(t *testing.T, replacement func(string, string) error) func() {
	t.Helper()
	original := renameFile
	renameFile = replacement
	return func() { renameFile = original }
}

func replaceRemove(t *testing.T, replacement func(string) error) func() {
	t.Helper()
	original := removeFile
	removeFile = replacement
	return func() { removeFile = original }
}

func replaceCreateTemp(t *testing.T, replacement func(string, string) (*os.File, error)) func() {
	t.Helper()
	original := createTempFile
	createTempFile = replacement
	return func() { createTempFile = original }
}
