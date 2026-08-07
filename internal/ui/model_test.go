package ui

import (
	"errors"
	"os"
	"path/filepath"
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

func TestJKNavigationRefreshesPreviewAndRememberedScopeSelection(t *testing.T) {
	model := New([]config.Prompt{
		{Name: "Local one", Description: "first", Contents: "local one body", Source: config.SourceProject},
		{Name: "Local two", Description: "second", Contents: "local two body", Source: config.SourceProject},
		{Name: "Global", Description: "global", Contents: "global body", Source: config.SourceGlobal},
	})
	model.setScope(localScope)

	model = update(t, model, key("j"))
	if model.currentPrompt().Name != "Local two" || model.preview.GetContent() != "local two body" {
		t.Fatalf("after j: selected=%q preview=%q", model.currentPrompt().Name, model.preview.GetContent())
	}
	model.setScope(globalScope)
	model.setScope(localScope)
	if model.currentPrompt().Name != "Local two" {
		t.Fatalf("remembered selection after j = %q", model.currentPrompt().Name)
	}

	model = update(t, model, key("k"))
	if model.currentPrompt().Name != "Local one" || model.preview.GetContent() != "local one body" {
		t.Fatalf("after k: selected=%q preview=%q", model.currentPrompt().Name, model.preview.GetContent())
	}
	model.setScope(globalScope)
	model.setScope(localScope)
	if model.currentPrompt().Name != "Local one" {
		t.Fatalf("remembered selection after k = %q", model.currentPrompt().Name)
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

func TestDescriptionlessPromptRendersAndSearchesByTitleAndBody(t *testing.T) {
	model := New([]config.Prompt{{Name: "Deploy helper", Contents: "production checklist", Source: config.SourceProject}})
	model = update(t, model, tea.WindowSizeMsg{Width: 80, Height: 20})
	if view := model.View().Content; !strings.Contains(view, "Deploy helper") {
		t.Fatalf("descriptionless prompt missing from list: %q", view)
	}
	for _, input := range []string{"p", "r", "o", "d"} {
		model = update(t, model, key(input))
	}
	if len(model.list.Items()) != 1 || !strings.Contains(model.list.Items()[0].(promptItem).description, "production checklist") {
		t.Errorf("body search did not show a useful excerpt: %#v", model.list.Items())
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

func TestPreviewWrapsAtWordBoundaries(t *testing.T) {
	got := wrapPreview("alpha beta gamma\n\ndelta", 10)
	want := "alpha beta\ngamma\n\ndelta"
	if got != want {
		t.Fatalf("wrapped preview = %q, want %q", got, want)
	}
}

func TestPreviewSplitsOverlongTokensSafely(t *testing.T) {
	got := wrapPreview("short supercalifragilistic", 8)
	want := "short\nsupercal\nifragili\nstic"
	if got != want {
		t.Fatalf("overlong preview = %q, want %q", got, want)
	}
}

func TestPreviewPreservesIndentationAndRepeatedWhitespace(t *testing.T) {
	got := wrapPreview("  alpha   beta\tgamma", 13)
	want := "  alpha   \nbeta\tgamma"
	if got != want {
		t.Fatalf("whitespace-preserving preview = %q, want %q", got, want)
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

func TestMalformedFileWarningIsNonfatalWhenValidPromptsExist(t *testing.T) {
	loadErr := errors.New("invalid local prompt in /work/.herdr/prompts/bad.md: malformed YAML")
	model := New(testPrompts(), loadErr)
	model = update(t, model, tea.WindowSizeMsg{Width: 70, Height: 20})
	view := model.View().Content
	for _, text := range []string{"Some prompts could not be loaded", "bad.md", "malformed YAML", "shared"} {
		if !strings.Contains(view, text) {
			t.Errorf("error view does not contain %q", text)
		}
	}
	_, command := model.Update(key("enter"))
	if command == nil {
		t.Error("warning prevented selection of a valid prompt")
	}
}

func TestCreatePromptsInBothScopesWithExactBody(t *testing.T) {
	for _, source := range []string{config.SourceProject, config.SourceGlobal} {
		t.Run(source, func(t *testing.T) {
			libraries := uiTestLibraries(t)
			model := NewWithLibraries(nil, libraries)
			if source == config.SourceGlobal {
				model.setScope(globalScope)
			} else {
				model.setScope(localScope)
			}
			model = update(t, model, key("alt+a"))
			if model.form == nil || model.form.destination != source {
				t.Fatalf("create destination = %#v, want %s", model.form, source)
			}
			if model.form.description.Placeholder != "Optional description" {
				t.Errorf("description placeholder = %q", model.form.description.Placeholder)
			}
			body := "  first line\nsecond line  \n"
			model.form.title.SetValue("New prompt")
			model.form.body.SetValue(body)
			model = update(t, model, key("ctrl+s"))
			if model.form != nil || len(model.prompts) != 1 {
				t.Fatalf("save form=%v prompts=%#v", model.form != nil, model.prompts)
			}
			created := model.prompts[0]
			if created.Source != source || created.Description != "" || created.Contents != body || model.preview.GetContent() != body {
				t.Errorf("created = %#v, preview = %q", created, model.preview.GetContent())
			}
			contents, err := os.ReadFile(created.Path)
			if err != nil || !strings.HasSuffix(string(contents), "---\n"+body) {
				t.Errorf("file contents = %q, err = %v", contents, err)
			}
		})
	}
}

func TestFormBodyAcceptsBracketedPasteInEveryMode(t *testing.T) {
	prompt := config.Prompt{
		Name:        "Original",
		Description: "Description",
		Contents:    "existing body\n",
		Source:      config.SourceProject,
		Path:        "/project/original.md",
	}
	pasted := "  ctrl+s\nesc    alt+d  \n  \n"
	tests := []struct {
		name     string
		shortcut string
		initial  string
	}{
		{name: "create", shortcut: "alt+a"},
		{name: "edit", shortcut: "alt+e", initial: prompt.Contents},
		{name: "duplicate", shortcut: "alt+u", initial: prompt.Contents},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := NewWithLibraries([]config.Prompt{prompt}, fakeLibraries{})
			model = update(t, model, key(test.shortcut))
			model.focusForm(2)
			destination := model.form.destination

			model = update(t, model, tea.PasteMsg{Content: pasted})
			model = update(t, model, key("x"))

			if model.form == nil {
				t.Fatal("pasted shortcut-looking text closed the form")
			}
			if model.confirmation != nil || model.form.destination != destination {
				t.Fatalf("paste triggered a popup action: confirmation=%v destination=%q", model.confirmation != nil, model.form.destination)
			}
			if got, want := model.form.body.Value(), test.initial+pasted+"x"; got != want {
				t.Errorf("body after paste and typing = %q, want %q", got, want)
			}
		})
	}
}

func TestFormTextInputsAcceptBracketedPaste(t *testing.T) {
	model := NewWithLibraries(nil, fakeLibraries{})
	model = update(t, model, key("alt+a"))
	model = update(t, model, tea.PasteMsg{Content: "ctrl+s"})
	model = update(t, model, key("x"))
	if got, want := model.form.title.Value(), "ctrl+sx"; got != want {
		t.Errorf("title after paste and typing = %q, want %q", got, want)
	}

	model = update(t, model, key("tab"))
	model = update(t, model, tea.PasteMsg{Content: "esc"})
	model = update(t, model, key("y"))
	if model.form == nil {
		t.Fatal("pasted shortcut-looking description closed the form")
	}
	if got, want := model.form.description.Value(), "escy"; got != want {
		t.Errorf("description after paste and typing = %q, want %q", got, want)
	}
}

func TestCrossScopeCreateRevealsResultAndPreservesMatchingQuery(t *testing.T) {
	libraries := uiTestLibraries(t)
	existing := mustCreatePrompt(t, libraries, config.SourceProject, "Existing", "body")
	model := NewWithLibraries([]config.Prompt{existing}, libraries)
	model.setScope(localScope)
	model.query = "New"
	model.refreshItems(config.Prompt{}, false)
	model = update(t, model, key("alt+a"))
	model.form.title.SetValue("New global prompt")
	model.form.description.SetValue("Description")
	model.form.body.SetValue("new body")
	model.form.destination = config.SourceGlobal
	model = update(t, model, key("ctrl+s"))

	if model.scope != globalScope || model.query != "New" || model.currentPrompt().Name != "New global prompt" {
		t.Fatalf("cross-scope create: scope=%s query=%q selected=%#v", model.scope, model.query, model.currentPrompt())
	}
	if model.preview.GetContent() != "new body" || len(model.list.Items()) != 1 {
		t.Errorf("cross-scope create preview=%q results=%d", model.preview.GetContent(), len(model.list.Items()))
	}
}

func TestCreateValidationAndWriteErrorsPreserveForm(t *testing.T) {
	libraries := uiTestLibraries(t)
	validationModel := NewWithLibraries(nil, libraries)
	validationModel = update(t, validationModel, key("alt+a"))
	validationModel = update(t, validationModel, key("ctrl+s"))
	if validationModel.form == nil || validationModel.form.err == nil || !strings.Contains(validationModel.form.err.Error(), "title must not be blank") {
		t.Fatalf("blank form validation = %#v", validationModel.form)
	}

	wantErr := errors.New("disk full")
	storage := fakeLibraries{createFunc: func(string, config.Prompt) (config.Prompt, error) {
		return config.Prompt{}, wantErr
	}}
	model := NewWithLibraries(nil, storage)
	model = update(t, model, key("alt+a"))
	model.form.title.SetValue("Keep title")
	model.form.description.SetValue("Keep description")
	model.form.body.SetValue("Keep\nbody")
	model = update(t, model, key("ctrl+s"))
	if model.form == nil || !errors.Is(model.form.err, wantErr) {
		t.Fatalf("form error = %#v", model.form)
	}
	if model.form.title.Value() != "Keep title" || model.form.description.Value() != "Keep description" || model.form.body.Value() != "Keep\nbody" {
		t.Errorf("form input was not preserved: %#v", model.form)
	}
	model = update(t, model, key("esc"))
	if model.form != nil || model.Cancelled() {
		t.Error("Esc did not cancel only the form")
	}
}

func TestEditGlobalAndUpdateFailurePreservesInput(t *testing.T) {
	libraries := uiTestLibraries(t)
	global := mustCreatePrompt(t, libraries, config.SourceGlobal, "Global", "global body")
	model := NewWithLibraries([]config.Prompt{global}, libraries)
	model = update(t, model, key("alt+e"))
	model.form.title.SetValue("Changed global")
	model = update(t, model, key("ctrl+s"))
	if model.currentPrompt().Source != config.SourceGlobal || model.currentPrompt().Name != "Changed global" {
		t.Errorf("global edit = %#v", model.currentPrompt())
	}

	wantErr := errors.New("write denied")
	storage := fakeLibraries{updateFunc: func(config.Prompt, config.Prompt) (config.Prompt, error) {
		return config.Prompt{}, wantErr
	}}
	model = NewWithLibraries([]config.Prompt{global}, storage)
	model = update(t, model, key("alt+e"))
	model.form.title.SetValue("Keep this edit")
	model.form.body.SetValue("keep exact\n")
	model = update(t, model, key("ctrl+s"))
	if model.form == nil || !errors.Is(model.form.err, wantErr) || model.form.title.Value() != "Keep this edit" || model.form.body.Value() != "keep exact\n" {
		t.Errorf("failed edit did not preserve form: %#v", model.form)
	}
}

func TestEditPreservesPathUnknownMetadataAndRefreshesPreview(t *testing.T) {
	libraries := uiTestLibraries(t)
	path := filepath.Join(libraries.LocalDir, "original.md")
	if err := os.MkdirAll(libraries.LocalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\ntitle: Original\ndescription: Before\ncustom: keep-me\n---\nold body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt := config.Prompt{Name: "Original", Description: "Before", Contents: "old body\n", Source: config.SourceProject, Path: path}
	model := NewWithLibraries([]config.Prompt{prompt}, libraries)
	model = update(t, model, key("alt+e"))
	if model.form == nil || model.form.body.Value() != "old body\n" {
		t.Fatalf("edit form = %#v", model.form)
	}
	model.form.title.SetValue("Renamed")
	model.form.description.SetValue("   ")
	model.form.body.SetValue(" exact body\n")
	model = update(t, model, key("ctrl+s"))
	updated := model.currentPrompt()
	if updated.Path != path || updated.Name != "Renamed" || updated.Description != "   " || model.preview.GetContent() != " exact body\n" {
		t.Errorf("updated = %#v preview=%q", updated, model.preview.GetContent())
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "custom: keep-me") {
		t.Errorf("unknown metadata missing from %q: %v", raw, err)
	}
}

func TestDeleteCancellationFilteredNearestAndLastResult(t *testing.T) {
	libraries := uiTestLibraries(t)
	first := mustCreatePrompt(t, libraries, config.SourceProject, "Keep first", "first")
	second := mustCreatePrompt(t, libraries, config.SourceProject, "Keep second", "second")
	model := NewWithLibraries([]config.Prompt{first, second}, libraries)
	model = update(t, model, tea.WindowSizeMsg{Width: 100, Height: 30})
	model.scope = localScope
	model.query = "Keep"
	model.refreshItems(second, true)
	model = update(t, model, key("alt+d"))
	if view := model.View().Content; !contains(view, "Delete prompt?", "Keep second", "Local", filepath.Base(second.Path)) || model.confirmation.prompt.Path != second.Path {
		t.Fatalf("confirmation missing context: %s", view)
	}
	model = update(t, model, key("esc"))
	if _, err := os.Stat(second.Path); err != nil {
		t.Fatalf("cancel removed file: %v", err)
	}
	model = update(t, model, key("alt+d"))
	model = update(t, model, key("enter"))
	if model.query != "Keep" || model.scope != localScope || model.currentPrompt().Path != first.Path {
		t.Errorf("post-delete query=%q scope=%s selected=%#v", model.query, model.scope, model.currentPrompt())
	}
	model = update(t, model, key("alt+d"))
	model = update(t, model, key("enter"))
	if len(model.list.Items()) != 0 || model.query != "Keep" {
		t.Errorf("last deletion left %d results, query %q", len(model.list.Items()), model.query)
	}
}

func TestDuplicatePrefillsAndAllowsCrossScopeDestination(t *testing.T) {
	libraries := uiTestLibraries(t)
	original := mustCreatePrompt(t, libraries, config.SourceProject, "Original", " exact\nbody\n")
	model := NewWithLibraries([]config.Prompt{original}, libraries)
	model = update(t, model, key("alt+u"))
	if model.form == nil || model.form.title.Value() != "Original copy" || model.form.body.Value() != original.Contents || model.form.destination != config.SourceProject {
		t.Fatalf("duplicate form = %#v", model.form)
	}
	model.form.destination = config.SourceGlobal
	model = update(t, model, key("ctrl+s"))
	if len(model.prompts) != 2 || model.currentPrompt().Source != config.SourceGlobal || model.currentPrompt().Contents != original.Contents {
		t.Errorf("duplicated prompts = %#v selected=%#v", model.prompts, model.currentPrompt())
	}
	if _, err := os.Stat(original.Path); err != nil {
		t.Errorf("original changed: %v", err)
	}
	before := len(model.prompts)
	model = update(t, model, key("alt+u"))
	model = update(t, model, key("esc"))
	if len(model.prompts) != before {
		t.Error("cancelled duplicate created a prompt")
	}
}

func TestDuplicatePreservesCustomFrontmatterAndRevealsCrossScopeResult(t *testing.T) {
	libraries := uiTestLibraries(t)
	path := filepath.Join(libraries.LocalDir, "original.md")
	if err := os.MkdirAll(libraries.LocalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "---\ntitle: Original\ndescription: Before\ncustom: keep-me\n---\nold body\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	original := config.Prompt{Name: "Original", Description: "Before", Contents: "old body\n", Source: config.SourceProject, Path: path}
	model := NewWithLibraries([]config.Prompt{original}, libraries)
	model.setScope(localScope)
	model.query = "Original"
	model.refreshItems(original, true)
	model = update(t, model, key("alt+u"))
	model.form.title.SetValue("Fresh copy")
	model.form.description.SetValue("")
	model.form.destination = config.SourceGlobal
	model = update(t, model, key("ctrl+s"))

	duplicate := model.currentPrompt()
	if model.scope != globalScope || model.query != "" || duplicate.Name != "Fresh copy" || duplicate.Description != "" || duplicate.Source != config.SourceGlobal {
		t.Fatalf("cross-scope duplicate: scope=%s query=%q selected=%#v", model.scope, model.query, duplicate)
	}
	contents, err := os.ReadFile(duplicate.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(contents), "title: Fresh copy", "description: \"\"", "custom: keep-me", "old body\n") {
		t.Errorf("duplicate did not retain custom frontmatter: %q", contents)
	}
}

func TestDuplicateSameScopeAndWriteFailure(t *testing.T) {
	libraries := uiTestLibraries(t)
	original := mustCreatePrompt(t, libraries, config.SourceGlobal, "Copy", "body")
	model := NewWithLibraries([]config.Prompt{original}, libraries)
	model = update(t, model, key("alt+u"))
	model = update(t, model, key("ctrl+s"))
	if len(model.prompts) != 2 || model.currentPrompt().Source != config.SourceGlobal || model.currentPrompt().Path == original.Path {
		t.Errorf("same-scope duplicate = %#v", model.prompts)
	}

	wantErr := errors.New("read-only library")
	storage := fakeLibraries{duplicateFunc: func(config.Prompt, string, config.Prompt) (config.Prompt, error) {
		return config.Prompt{}, wantErr
	}}
	model = NewWithLibraries([]config.Prompt{original}, storage)
	model = update(t, model, key("alt+u"))
	model.form.title.SetValue("Copy retry")
	model = update(t, model, key("ctrl+s"))
	if model.form == nil || !errors.Is(model.form.err, wantErr) || model.form.title.Value() != "Copy retry" {
		t.Errorf("failed duplicate form = %#v", model.form)
	}
}

func TestDeleteGlobalAndFailureRetainsEntry(t *testing.T) {
	libraries := uiTestLibraries(t)
	global := mustCreatePrompt(t, libraries, config.SourceGlobal, "Global delete", "body")
	model := NewWithLibraries([]config.Prompt{global}, libraries)
	model.setScope(globalScope)
	model = update(t, model, key("alt+d"))
	model = update(t, model, key("enter"))
	if len(model.prompts) != 0 {
		t.Errorf("global deletion retained prompts: %#v", model.prompts)
	}

	wantErr := errors.New("permission denied")
	storage := fakeLibraries{deleteFunc: func(config.Prompt) error { return wantErr }}
	model = NewWithLibraries([]config.Prompt{global}, storage)
	model = update(t, model, key("alt+d"))
	model = update(t, model, key("enter"))
	if model.confirmation == nil || !errors.Is(model.confirmation.err, wantErr) || len(model.prompts) != 1 {
		t.Errorf("failed deletion state: confirmation=%#v prompts=%#v", model.confirmation, model.prompts)
	}
}

func TestEscapeUnwindsFormThenQueryThenPopup(t *testing.T) {
	libraries := uiTestLibraries(t)
	prompt := mustCreatePrompt(t, libraries, config.SourceProject, "Prompt", "body")
	model := NewWithLibraries([]config.Prompt{prompt}, libraries)
	model.query = "Prompt"
	model.refreshItems(prompt, true)
	model = update(t, model, key("alt+a"))
	model = update(t, model, key("esc"))
	if model.form != nil || model.query != "Prompt" || model.Cancelled() {
		t.Fatalf("form escape: form=%v query=%q cancelled=%v", model.form != nil, model.query, model.Cancelled())
	}
	model = update(t, model, key("esc"))
	if model.query != "" || model.Cancelled() {
		t.Fatalf("query escape: query=%q cancelled=%v", model.query, model.Cancelled())
	}
	model = update(t, model, key("esc"))
	if !model.Cancelled() {
		t.Error("third Esc did not close popup")
	}
}

func TestManagerImmediatelySearchesFormerActionLetters(t *testing.T) {
	libraries := uiTestLibraries(t)
	for _, query := range []string{"alpha", "edit", "delete", "Duplicate", "move"} {
		t.Run(query, func(t *testing.T) {
			prompt := mustCreatePrompt(t, libraries, config.SourceProject, query, "body")
			model := NewWithLibraries([]config.Prompt{prompt}, libraries)
			for _, character := range query {
				model = update(t, model, key(string(character)))
			}
			if model.query != query || model.form != nil || model.confirmation != nil || len(model.list.Items()) != 1 {
				t.Fatalf("typed query=%q form=%v confirmation=%v results=%d", model.query, model.form != nil, model.confirmation != nil, len(model.list.Items()))
			}
			model = update(t, model, key("backspace"))
			last := string([]rune(query)[len([]rune(query))-1])
			model = update(t, model, key(last))
			updated, command := model.Update(key("enter"))
			model = updated.(Model)
			if command == nil {
				t.Fatal("Enter did not immediately select the filtered prompt")
			}
			selection, ok := command().(SelectionMsg)
			if !ok || selection.Prompt.Path != prompt.Path {
				t.Fatalf("Enter message = %#v, want selected prompt", selection)
			}
			model = update(t, model, key("esc"))
			if model.query != "" || model.Cancelled() {
				t.Fatalf("Esc did not clear query first: query=%q cancelled=%v", model.query, model.Cancelled())
			}
		})
	}
}

func TestModifiedManagementShortcutsOpenExpectedFlows(t *testing.T) {
	libraries := uiTestLibraries(t)
	prompt := mustCreatePrompt(t, libraries, config.SourceProject, "Prompt", "body")
	tests := []struct {
		key              string
		formMode         formMode
		confirmationKind confirmationKind
	}{
		{key: "alt+a", formMode: createForm},
		{key: "alt+e", formMode: editForm},
		{key: "alt+d", confirmationKind: deleteConfirmation},
		{key: "alt+u", formMode: duplicateForm},
		{key: "alt+m", confirmationKind: moveConfirmation},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			model := NewWithLibraries([]config.Prompt{prompt}, libraries)
			model = update(t, model, key(test.key))
			if test.formMode != 0 && (model.form == nil || model.form.mode != test.formMode) {
				t.Fatalf("form = %#v, want mode %v", model.form, test.formMode)
			}
			if test.confirmationKind != 0 && (model.confirmation == nil || model.confirmation.kind != test.confirmationKind) {
				t.Fatalf("confirmation = %#v, want kind %v", model.confirmation, test.confirmationKind)
			}
		})
	}
}

func TestMoveBothDirectionsAndPartialFailure(t *testing.T) {
	for _, source := range []string{config.SourceProject, config.SourceGlobal} {
		t.Run(source, func(t *testing.T) {
			libraries := uiTestLibraries(t)
			prompt := mustCreatePrompt(t, libraries, source, "Move me", "body")
			model := NewWithLibraries([]config.Prompt{prompt}, libraries)
			model = update(t, model, tea.WindowSizeMsg{Width: 100, Height: 30})
			model.query = "Move"
			model.refreshItems(prompt, true)
			model = update(t, model, key("alt+m"))
			wantDestination := "Global"
			if source == config.SourceGlobal {
				wantDestination = "Local"
			}
			if view := model.View().Content; !contains(view, "Move prompt?", "Destination: "+wantDestination, filepath.Base(prompt.Path)) || model.confirmation.prompt.Path != prompt.Path {
				t.Errorf("move confirmation missing context: %s", view)
			}
			model = update(t, model, key("enter"))
			moved := model.currentPrompt()
			if moved.Source != otherSource(source) || moved.Contents != prompt.Contents || model.query != "Move" {
				t.Errorf("moved = %#v query=%q", moved, model.query)
			}
		})
	}

	source := config.Prompt{Name: "Partial", Description: "Description", Contents: "body", Source: config.SourceProject, Path: "/local/partial.md"}
	destination := source
	destination.Source = config.SourceGlobal
	destination.Path = "/global/partial.md"
	partial := &config.PartialMoveError{SourcePath: source.Path, DestinationPath: destination.Path, Err: errors.New("permission denied")}
	storage := fakeLibraries{moveFunc: func(config.Prompt, string) (config.Prompt, error) { return destination, partial }}
	model := NewWithLibraries([]config.Prompt{source}, storage)
	model = update(t, model, key("alt+m"))
	model = update(t, model, key("enter"))
	if len(model.prompts) != 2 || model.operationErr == nil || !contains(model.operationErr.Error(), source.Path, destination.Path, "both files remain") {
		t.Errorf("partial move prompts=%#v err=%v", model.prompts, model.operationErr)
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
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	case "alt+a":
		return tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt}
	case "alt+e":
		return tea.KeyPressMsg{Code: 'e', Mod: tea.ModAlt}
	case "alt+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModAlt}
	case "alt+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModAlt}
	case "alt+m":
		return tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	default:
		return tea.KeyPressMsg{Code: []rune(value)[0], Text: value}
	}
}

type fakeLibraries struct {
	createFunc    func(string, config.Prompt) (config.Prompt, error)
	updateFunc    func(config.Prompt, config.Prompt) (config.Prompt, error)
	deleteFunc    func(config.Prompt) error
	duplicateFunc func(config.Prompt, string, config.Prompt) (config.Prompt, error)
	moveFunc      func(config.Prompt, string) (config.Prompt, error)
}

func (fake fakeLibraries) Create(source string, prompt config.Prompt) (config.Prompt, error) {
	if fake.createFunc == nil {
		return config.Prompt{}, errors.New("unexpected Create")
	}
	return fake.createFunc(source, prompt)
}

func (fake fakeLibraries) Update(prompt, changes config.Prompt) (config.Prompt, error) {
	if fake.updateFunc == nil {
		return config.Prompt{}, errors.New("unexpected Update")
	}
	return fake.updateFunc(prompt, changes)
}

func (fake fakeLibraries) Delete(prompt config.Prompt) error {
	if fake.deleteFunc == nil {
		return errors.New("unexpected Delete")
	}
	return fake.deleteFunc(prompt)
}

func (fake fakeLibraries) Duplicate(prompt config.Prompt, destination string, changes config.Prompt) (config.Prompt, error) {
	if fake.duplicateFunc == nil {
		return config.Prompt{}, errors.New("unexpected Duplicate")
	}
	return fake.duplicateFunc(prompt, destination, changes)
}

func (fake fakeLibraries) Move(prompt config.Prompt, destination string) (config.Prompt, error) {
	if fake.moveFunc == nil {
		return config.Prompt{}, errors.New("unexpected Move")
	}
	return fake.moveFunc(prompt, destination)
}

func uiTestLibraries(t *testing.T) config.Libraries {
	t.Helper()
	root := t.TempDir()
	return config.Libraries{
		LocalDir:  filepath.Join(root, "local", "prompts"),
		GlobalDir: filepath.Join(root, "global", "prompts"),
	}
}

func mustCreatePrompt(t *testing.T, libraries config.Libraries, source, title, body string) config.Prompt {
	t.Helper()
	prompt, err := libraries.Create(source, config.Prompt{Name: title, Description: "Description", Contents: body})
	if err != nil {
		t.Fatal(err)
	}
	return prompt
}

func contains(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
