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
	model = update(t, model, key("up"))
	if model.list.Index() != 0 || model.preview.GetContent() != "project contents\nline 2" {
		t.Fatalf("after up: index = %d, preview = %q", model.list.Index(), model.preview.GetContent())
	}
	model = update(t, model, key("down"))
	model = update(t, model, key("up"))
	if model.list.Index() != 0 {
		t.Errorf("down then up index = %d, want 0", model.list.Index())
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

func TestEscapeAndCtrlCCancelWithoutSelection(t *testing.T) {
	for _, input := range []string{"esc", "ctrl+c"} {
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

func TestWeightedFuzzySearchRanksFieldsAndShowsBodyExcerpt(t *testing.T) {
	model := New([]config.Prompt{
		{Name: "Body match", Description: "Only in contents", Contents: "Run the deploy production checklist carefully", Source: config.SourceProject},
		{Name: "Description match", Description: "Deploy production safely", Contents: "unrelated", Source: config.SourceGlobal},
		{Name: "Deploy production", Description: "Title match", Contents: "unrelated", Source: config.SourceProject},
	}, nil)
	model = update(t, model, tea.WindowSizeMsg{Width: 100, Height: 24})
	for _, input := range []string{"d", "p", "l"} {
		model = update(t, model, key(input))
	}

	items := model.list.Items()
	if len(items) != 3 {
		t.Fatalf("filtered item count = %d, want 3", len(items))
	}
	want := []string{"Deploy production", "Description match", "Body match"}
	for index, name := range want {
		if got := items[index].(promptItem).prompt.Name; got != name {
			t.Errorf("result %d = %q, want %q", index, got, name)
		}
	}
	bodyDescription := items[2].(promptItem).description
	if !strings.Contains(bodyDescription, "deploy production checklist") {
		t.Errorf("body-only result description = %q, want matching excerpt", bodyDescription)
	}
}

func TestEscapeClearsQueryBeforeClosing(t *testing.T) {
	model := newTestModel(t)
	model = update(t, model, key("g"))
	if model.query != "g" || len(model.list.Items()) != 1 {
		t.Fatalf("search state = %q, %d items", model.query, len(model.list.Items()))
	}
	updated, command := model.Update(key("esc"))
	model = updated.(Model)
	if command != nil || model.query != "" || model.Cancelled() || len(model.list.Items()) != 2 {
		t.Fatalf("first Esc: command=%v query=%q cancelled=%v items=%d", command != nil, model.query, model.Cancelled(), len(model.list.Items()))
	}
	updated, command = model.Update(key("esc"))
	model = updated.(Model)
	if command == nil || !model.Cancelled() {
		t.Fatal("second Esc did not close the picker")
	}
}

func TestFilteredNavigationPreviewAndInsertionUseExactPrompt(t *testing.T) {
	model := New([]config.Prompt{
		{Name: "Local deploy", Description: "deploy", Contents: "local body", Source: config.SourceProject},
		{Name: "Global deploy", Description: "deploy", Contents: "global body", Source: config.SourceGlobal},
		{Name: "Other", Description: "other", Contents: "other body", Source: config.SourceGlobal},
	}, nil)
	for _, input := range []string{"d", "p", "l"} {
		model = update(t, model, key(input))
	}
	model = update(t, model, key("down"))
	if got := model.preview.GetContent(); got != "global body" {
		t.Fatalf("filtered preview = %q, want global body", got)
	}
	updated, command := model.Update(key("enter"))
	model = updated.(Model)
	message := command().(SelectionMsg)
	if message.Prompt.Name != "Global deploy" || message.Prompt.Source != config.SourceGlobal {
		t.Errorf("filtered selection = %#v", message.Prompt)
	}
}

func TestScopesCycleDirectlyFilterAndRememberSelection(t *testing.T) {
	model := New([]config.Prompt{
		{Name: "Local one", Description: "first", Contents: "local one", Source: config.SourceProject},
		{Name: "Local two", Description: "shared term", Contents: "local two", Source: config.SourceProject},
		{Name: "Global one", Description: "first", Contents: "global one", Source: config.SourceGlobal},
		{Name: "Global two", Description: "shared term", Contents: "global two", Source: config.SourceGlobal},
	}, nil)
	model = update(t, model, key("tab"))
	if model.scope != localScope || len(model.list.Items()) != 2 {
		t.Fatalf("Tab scope = %s with %d items", model.scope, len(model.list.Items()))
	}
	model = update(t, model, key("tab"))
	if model.scope != globalScope || len(model.list.Items()) != 2 {
		t.Fatalf("second Tab scope = %s with %d items", model.scope, len(model.list.Items()))
	}
	model = update(t, model, key("tab"))
	if model.scope != allScope || len(model.list.Items()) != 4 {
		t.Fatalf("third Tab scope = %s with %d items", model.scope, len(model.list.Items()))
	}
	model = update(t, model, key("ctrl+l"))
	model = update(t, model, key("down"))
	model = update(t, model, key("ctrl+g"))
	if model.scope != globalScope || len(model.list.Items()) != 2 {
		t.Fatalf("direct global scope = %s with %d items", model.scope, len(model.list.Items()))
	}
	model = update(t, model, key("down"))
	model = update(t, model, key("ctrl+l"))
	if got := model.currentPrompt().Name; got != "Local two" {
		t.Errorf("remembered local selection = %q", got)
	}
	model = update(t, model, key("ctrl+g"))
	if got := model.currentPrompt().Name; got != "Global two" {
		t.Errorf("remembered global selection = %q", got)
	}
	for _, input := range []string{"s", "h", "a", "r", "e", "d"} {
		model = update(t, model, key(input))
	}
	if len(model.list.Items()) != 1 || model.currentPrompt().Name != "Global two" {
		t.Fatalf("global query results = %d, selected %q", len(model.list.Items()), model.currentPrompt().Name)
	}
	model = update(t, model, key("ctrl+a"))
	if model.scope != allScope || len(model.list.Items()) != 2 {
		t.Fatalf("all query results = %d in %s", len(model.list.Items()), model.scope)
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
	for _, text := range []string{"shared", "Project duplicate", "LOCAL", "GLOBAL", "Preview", "project contents", "All", "Local", "Global", "Search:"} {
		if !strings.Contains(wide, text) {
			t.Errorf("wide view does not contain %q:\n%s", text, wide)
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
	for _, text := range []string{"No prompts found", "local or global library"} {
		if !strings.Contains(view, text) {
			t.Errorf("empty view does not contain %q", text)
		}
	}
	_, command := model.Update(key("enter"))
	if command != nil {
		t.Error("empty picker emitted a selection")
	}
}

func TestEmptyScopeAndNoMatchesExplainRecovery(t *testing.T) {
	model := New([]config.Prompt{{Name: "Only global", Description: "available", Contents: "body", Source: config.SourceGlobal}}, nil)
	model = update(t, model, tea.WindowSizeMsg{Width: 70, Height: 20})
	model = update(t, model, key("ctrl+l"))
	view := model.View().Content
	for _, text := range []string{"No local prompts found", "Tab", "Ctrl+A"} {
		if !strings.Contains(view, text) {
			t.Errorf("empty scope view does not contain %q", text)
		}
	}
	model = update(t, model, key("ctrl+a"))
	model = update(t, model, key("z"))
	view = model.View().Content
	for _, text := range []string{"No prompts match", "Backspace", "Esc", "Tab"} {
		if !strings.Contains(view, text) {
			t.Errorf("no-match view does not contain %q", text)
		}
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
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+a":
		return tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+g":
		return tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}
	case "ctrl+l":
		return tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	default:
		return tea.KeyPressMsg{Code: []rune(value)[0], Text: value}
	}
}
