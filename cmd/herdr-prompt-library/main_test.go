package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"herdr-prompt-library/internal/config"
	"herdr-prompt-library/internal/herdr"
	"herdr-prompt-library/internal/ui"
)

func TestPickerModelInsertsExactPromptIntoCapturedPane(t *testing.T) {
	contents := "first line\nsecond line  \n$HOME; $(not-a-command) & 'quoted'\t "
	var gotName string
	var gotArgs []string
	model, err := pickerModel(env(herdr.TargetPaneIDEnv, "pane-at-open"), func() ([]config.Prompt, error) {
		return []config.Prompt{{Name: "exact", Description: "exact bytes", Contents: contents}}, nil
	}, testConfiguredLibraries, herdr.Client{Binary: "/tmp/herdr", Run: func(name string, args []string, _ []string) error {
		gotName, gotArgs = name, args
		return nil
	}})
	if err != nil {
		t.Fatalf("pickerModel() error = %v", err)
	}

	updated, command := model.Update(keyEnter())
	model = updated.(ui.Model)
	selection, ok := command().(ui.SelectionMsg)
	if !ok {
		t.Fatalf("enter message = %T, want SelectionMsg", command())
	}
	updated, command = model.Update(selection)
	model = updated.(ui.Model)
	result, ok := command().(ui.InsertionResultMsg)
	if !ok || result.Err != nil {
		t.Fatalf("insertion result = %#v", result)
	}
	updated, quit := model.Update(result)
	model = updated.(ui.Model)
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Errorf("successful insertion command = %T, want tea.QuitMsg", quit())
	}
	if gotName != "/tmp/herdr" {
		t.Errorf("binary = %q, want configured binary", gotName)
	}
	wantArgs := []string{"pane", "send-text", "pane-at-open", contents}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Errorf("arguments = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestPickerModelUsesDefaultBinaryAndStaysOpenOnInsertionFailure(t *testing.T) {
	want := errors.New("target pane disappeared")
	var gotName string
	model, err := pickerModel(env(herdr.TargetPaneIDEnv, "captured-pane"), func() ([]config.Prompt, error) {
		return []config.Prompt{{Name: "prompt", Description: "desc", Contents: "contents"}}, nil
	}, testConfiguredLibraries, herdr.Client{Run: func(name string, _ []string, _ []string) error {
		gotName = name
		return want
	}})
	if err != nil {
		t.Fatalf("pickerModel() error = %v", err)
	}
	updated, command := model.Update(keyEnter())
	model = updated.(ui.Model)
	updated, command = model.Update(command())
	model = updated.(ui.Model)
	result := command().(ui.InsertionResultMsg)
	if !errors.Is(result.Err, want) {
		t.Fatalf("insertion result error = %v, want %v", result.Err, want)
	}
	updated, quit := model.Update(result)
	model = updated.(ui.Model)
	if quit != nil {
		t.Error("failed insertion quit the picker")
	}
	if _, selected := model.SelectedPrompt(); selected {
		t.Error("failed insertion selected a prompt")
	}
	if gotName != herdr.DefaultBinary {
		t.Errorf("binary = %q, want fallback %q", gotName, herdr.DefaultBinary)
	}
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(ui.Model)
	if view := model.View().Content; !containsAll(view, "Could not insert prompt", "target pane disappeared", "press Enter to retry") {
		t.Errorf("failure view is not actionable: %q", view)
	}
}

func TestPickerModelRejectsMissingCapturedPane(t *testing.T) {
	_, err := pickerModel(env(), func() ([]config.Prompt, error) { return nil, nil }, testConfiguredLibraries, herdr.Client{})
	if err == nil || err.Error() != "open prompt picker: missing HERDR_PROMPT_LIBRARY_TARGET_PANE_ID" {
		t.Errorf("pickerModel() error = %v, want missing-pane error", err)
	}
}

func TestPickerModelInjectsConfiguredLibrariesAndReportsConfigurationFailure(t *testing.T) {
	configuredErr := errors.New("cannot determine cwd")
	loadCalled := false
	_, err := pickerModel(env(herdr.TargetPaneIDEnv, "pane"), func() ([]config.Prompt, error) {
		loadCalled = true
		return nil, nil
	}, func() (config.Libraries, error) {
		return config.Libraries{}, configuredErr
	}, herdr.Client{})
	if !errors.Is(err, configuredErr) || !strings.Contains(err.Error(), "configure prompt libraries") {
		t.Fatalf("pickerModel() error = %v", err)
	}
	if loadCalled {
		t.Error("prompt load ran after library configuration failed")
	}

	model, err := pickerModel(env(herdr.TargetPaneIDEnv, "pane"), func() ([]config.Prompt, error) {
		return nil, nil
	}, testConfiguredLibraries, herdr.Client{})
	if err != nil {
		t.Fatal(err)
	}
	updated, command := model.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt})
	if command != nil || !strings.Contains(updated.(ui.Model).View().Content, "Create prompt") {
		t.Error("configured libraries did not enable popup management")
	}
}

func keyEnter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }

func testConfiguredLibraries() (config.Libraries, error) {
	return config.Libraries{LocalDir: "/tmp/local", GlobalDir: "/tmp/global"}, nil
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

func TestOpenRejectsMissingFocusedPane(t *testing.T) {
	called := false
	err := open(env("HERDR_PLUGIN_CONTEXT_JSON", `{"workspace_cwd":"/project"}`), herdr.Client{
		Run: func(string, []string, []string) error {
			called = true
			return nil
		},
	})
	if err == nil || err.Error() != "open prompt library: no focused pane in plugin context" {
		t.Errorf("open() error = %v, want clear missing-pane error", err)
	}
	if called {
		t.Error("open() ran Herdr without a focused pane")
	}
}

func TestOpenRootFallbacksAndArgumentPropagation(t *testing.T) {
	tests := []struct {
		name   string
		json   string
		target string
		root   string
	}{
		{
			name:   "worktree checkout path",
			json:   `{"focused_pane_id":"pane; $(nope)","focused_pane_cwd":"/pane","workspace_cwd":"/workspace","worktree":{"checkout_path":"/checkout with spaces; $HOME"}}`,
			target: "pane; $(nope)",
			root:   "/checkout with spaces; $HOME",
		},
		{
			name:   "workspace cwd",
			json:   `{"focused_pane_id":"pane-1","focused_pane_cwd":"/pane","workspace_cwd":"/workspace"}`,
			target: "pane-1",
			root:   "/workspace",
		},
		{
			name:   "focused pane cwd",
			json:   `{"focused_pane_id":"pane-1","focused_pane_cwd":"/pane"}`,
			target: "pane-1",
			root:   "/pane",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotArgs []string
			err := open(env("HERDR_PLUGIN_CONTEXT_JSON", test.json), herdr.Client{
				Binary: "herdr test binary",
				Run: func(_ string, args []string, _ []string) error {
					gotArgs = args
					return nil
				},
			})
			if err != nil {
				t.Fatalf("open() error = %v", err)
			}
			wantArgs := []string{
				"plugin", "pane", "open", "--plugin", herdr.PluginID, "--entrypoint", herdr.PickerEntrypoint,
				"--env", herdr.TargetPaneIDEnv + "=" + test.target,
				"--env", herdr.ProjectRootEnv + "=" + test.root,
			}
			if !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Errorf("arguments = %#v, want %#v", gotArgs, wantArgs)
			}
		})
	}
}

func TestOpenRejectsMissingContext(t *testing.T) {
	err := open(env(), herdr.Client{})
	if err == nil || err.Error() != "open prompt library: missing HERDR_PLUGIN_CONTEXT_JSON" {
		t.Errorf("open() error = %v, want clear missing-context error", err)
	}
}

func env(values ...string) func(string) string {
	return func(key string) string {
		for i := 0; i < len(values); i += 2 {
			if values[i] == key {
				return values[i+1]
			}
		}
		return ""
	}
}

func TestCommandRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{nil, {}, {"unknown"}, {"open", "extra"}} {
		if err := command(args); err == nil {
			t.Errorf("command(%v) unexpectedly succeeded", args)
		} else if !errors.Is(err, errUsage) {
			t.Errorf("command(%v) error = %v, want usage error", args, err)
		}
	}
}

func TestManifestEntrypointsUseExplicitRelativePaths(t *testing.T) {
	path := filepath.Join("..", "..", "herdr-plugin.toml")
	manifest, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, want := range []string{
		`id = "` + herdr.PluginID + `"`,
		`command = ["./bin/herdr-prompt-library", "open"]`,
		`command = ["./bin/herdr-prompt-library", "picker"]`,
	} {
		if !strings.Contains(string(manifest), want) {
			t.Errorf("manifest does not contain %q", want)
		}
	}
}

func TestReadmeDocumentsQualifiedManifestActionID(t *testing.T) {
	readmePath := filepath.Join("..", "..", "README.md")
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}
	qualifiedActionID := herdr.PluginID + ".open"
	for _, want := range []string{
		"type = \"plugin_action\"\ncommand = \"" + qualifiedActionID + "\"",
		"herdr plugin action invoke " + qualifiedActionID,
	} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("README does not document %q", want)
		}
	}
	obsoleteKeybinding := "type = \"plugin_action\"\ncommand = \"" + herdr.PluginID + ":open\""
	if strings.Contains(string(readme), obsoleteKeybinding) {
		t.Errorf("README documents obsolete colon-qualified action keybinding %q", obsoleteKeybinding)
	}
}
