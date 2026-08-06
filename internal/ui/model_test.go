package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"herdr-prompt-library/internal/config"
)

func TestNavigationChangesSelectionAndPreview(t *testing.T) {
	model := newTestModel(t)
	if got := model.preview.GetContent(); got != "project contents\nline 2" {
		t.Fatalf("initial preview = %q", got)
	}

	model = update(t, model, key("down"))
	if model.list.Index() != 1 || model.preview.GetContent() != "global contents" {
		t.Fatalf("after down: index = %d, preview = %q", model.list.Index(), model.preview.GetContent())
	}
	model = update(t, model, key("k"))
	if model.list.Index() != 0 || model.preview.GetContent() != "project contents\nline 2" {
		t.Fatalf("after k: index = %d, preview = %q", model.list.Index(), model.preview.GetContent())
	}
	model = update(t, model, key("j"))
	model = update(t, model, key("up"))
	if model.list.Index() != 0 {
		t.Errorf("j then up index = %d, want 0", model.list.Index())
	}
}

func TestEnterEmitsExactSelectionForDuplicateName(t *testing.T) {
	model := newTestModel(t)
	model = update(t, model, key("down"))

	updated, command := model.Update(key("enter"))
	model = updated.(Model)
	if command == nil {
		t.Fatal("enter returned no selection command")
	}
	message, ok := command().(SelectionMsg)
	if !ok {
		t.Fatalf("enter message = %T, want SelectionMsg", command())
	}
	if message.Prompt.Source != config.SourceGlobal || message.Prompt.Contents != "global contents" {
		t.Errorf("selected prompt = %#v, want global duplicate", message.Prompt)
	}

	updated, quit := model.Update(message)
	model = updated.(Model)
	selected, ok := model.SelectedPrompt()
	if !ok || selected != message.Prompt {
		t.Errorf("SelectedPrompt() = %#v, %v", selected, ok)
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Errorf("selection follow-up = %T, want tea.QuitMsg", quit())
	}
}

func TestEscapeAndQCancelWithoutSelection(t *testing.T) {
	for _, input := range []string{"esc", "q"} {
		t.Run(input, func(t *testing.T) {
			model := newTestModel(t)
			updated, command := model.Update(key(input))
			model = updated.(Model)
			if !model.Cancelled() {
				t.Error("Cancelled() = false")
			}
			if _, selected := model.SelectedPrompt(); selected {
				t.Error("cancelled picker has a selection")
			}
			if command == nil {
				t.Fatal("cancel returned no command")
			}
			if _, ok := command().(tea.QuitMsg); !ok {
				t.Errorf("cancel command = %T, want tea.QuitMsg", command())
			}
		})
	}
}

func TestPreviewScrollingIsIndependentAndResetsOnNavigation(t *testing.T) {
	lines := make([]string, 30)
	for index := range lines {
		lines[index] = "line"
	}
	model := New([]config.Prompt{
		{Name: "long", Description: "Long", Contents: strings.Join(lines, "\n"), Source: config.SourceProject},
		{Name: "other", Description: "Other", Contents: "other", Source: config.SourceGlobal},
	}, nil)
	model = update(t, model, tea.WindowSizeMsg{Width: 100, Height: 14})
	model = update(t, model, key("pgdown"))
	if model.preview.YOffset() == 0 {
		t.Fatal("pgdown did not scroll preview")
	}
	if model.list.Index() != 0 {
		t.Errorf("pgdown moved list to %d", model.list.Index())
	}
	model = update(t, model, key("down"))
	if model.preview.YOffset() != 0 || model.preview.GetContent() != "other" {
		t.Errorf("navigation did not reset preview: offset = %d content = %q", model.preview.YOffset(), model.preview.GetContent())
	}
}

func TestResizeSwitchesLayoutsAndRecalculatesComponents(t *testing.T) {
	model := newTestModel(t)
	model = update(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})
	if !model.wide {
		t.Fatal("120-column model is not wide")
	}
	wideListWidth, widePreviewWidth := model.list.Width(), model.preview.Width()
	if wideListWidth <= 0 || widePreviewWidth <= wideListWidth {
		t.Errorf("wide dimensions: list = %d preview = %d", wideListWidth, widePreviewWidth)
	}

	model = update(t, model, tea.WindowSizeMsg{Width: 60, Height: 25})
	if model.wide {
		t.Fatal("60-column model is wide")
	}
	if model.list.Width() <= wideListWidth || model.preview.Width() != model.list.Width() {
		t.Errorf("stacked widths: list = %d preview = %d; previous list = %d", model.list.Width(), model.preview.Width(), wideListWidth)
	}
	if model.list.Height() <= 0 || model.preview.Height() <= 0 {
		t.Errorf("stacked heights: list = %d preview = %d", model.list.Height(), model.preview.Height())
	}
}

func TestViewShowsSourcesAndResponsiveLayout(t *testing.T) {
	model := newTestModel(t)
	model = update(t, model, tea.WindowSizeMsg{Width: 120, Height: 25})
	wide := model.View().Content
	for _, text := range []string{"shared", "Project duplicate", "PROJECT", "GLOBAL", "Preview", "project contents"} {
		if !strings.Contains(wide, text) {
			t.Errorf("wide view does not contain %q", text)
		}
	}
	if strings.Count(wide, "╭") < 2 {
		t.Error("wide view does not contain two bordered panels")
	}

	model = update(t, model, tea.WindowSizeMsg{Width: 60, Height: 25})
	stacked := model.View().Content
	if strings.Count(stacked, "╭") < 2 || wide == stacked {
		t.Error("narrow view was not rendered as a distinct stacked layout")
	}
}

func TestEmptyStateIsActionableAndCannotSelect(t *testing.T) {
	model := New(nil, nil)
	model = update(t, model, tea.WindowSizeMsg{Width: 70, Height: 20})
	view := model.View().Content
	for _, text := range []string{"No prompts found", ".herdr/prompts.toml", "HERDR_PLUGIN_CONFIG_DIR"} {
		if !strings.Contains(view, text) {
			t.Errorf("empty view does not contain %q", text)
		}
	}
	_, command := model.Update(key("enter"))
	if command != nil {
		t.Error("empty picker emitted a selection")
	}
}

func TestConfigurationErrorStateIsActionableAndCannotSelect(t *testing.T) {
	loadErr := errors.New("load project prompts from /work/.herdr/prompts.toml: malformed TOML")
	model := New(testPrompts(), loadErr)
	model = update(t, model, tea.WindowSizeMsg{Width: 70, Height: 20})
	view := model.View().Content
	for _, text := range []string{"Could not load prompts", "load project prompts", "malformed", "TOML", ".herdr/prompts.toml", "HERDR_PLUGIN_CONFIG_DIR"} {
		if !strings.Contains(view, text) {
			t.Errorf("error view does not contain %q", text)
		}
	}
	_, command := model.Update(key("enter"))
	if command != nil {
		t.Error("errored picker emitted a selection")
	}
}

func newTestModel(t *testing.T) Model {
	t.Helper()
	model := New(testPrompts(), nil)
	return update(t, model, tea.WindowSizeMsg{Width: 100, Height: 24})
}

func testPrompts() []config.Prompt {
	return []config.Prompt{
		{Name: "shared", Description: "Project duplicate", Contents: "project contents\nline 2", Source: config.SourceProject},
		{Name: "shared", Description: "Global duplicate", Contents: "global contents", Source: config.SourceGlobal},
	}
}

func update(t *testing.T, model Model, message tea.Msg) Model {
	t.Helper()
	updated, _ := model.Update(message)
	return updated.(Model)
}

func key(value string) tea.KeyPressMsg {
	switch value {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	default:
		return tea.KeyPressMsg{Code: []rune(value)[0], Text: value}
	}
}
