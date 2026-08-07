// Package ui implements the interactive prompt picker.
package ui

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"herdr-prompt-library/internal/config"
)

const (
	wideLayoutMinimum = 90
	outerPadding      = 1
	pickerPanelGap    = outerPadding
	panelGap          = 2
	panelInnerPadding = 1
	chromeHeight      = 6
	minimumPanelSize  = 3
	searchCursor      = "▏"
)

// SelectionMsg requests insertion of the selected prompt. The complete prompt
// is carried so prompts with the same name remain distinct.
type SelectionMsg struct {
	Prompt config.Prompt
}

// InsertionResultMsg reports the result of inserting a selected prompt.
type InsertionResultMsg struct {
	Prompt config.Prompt
	Err    error
}

type promptItem struct {
	prompt config.Prompt
}

// LibraryMutator is the storage used by prompt management actions.
type LibraryMutator interface {
	Create(string, config.Prompt) (config.Prompt, error)
	Update(config.Prompt, config.Prompt) (config.Prompt, error)
	Delete(config.Prompt) error
	Duplicate(config.Prompt, string, config.Prompt) (config.Prompt, error)
	Move(config.Prompt, string) (config.Prompt, error)
}

type formMode int

const (
	createForm formMode = iota + 1
	editForm
	duplicateForm
)

type promptForm struct {
	mode        formMode
	title       textinput.Model
	body        textarea.Model
	destination string
	focus       int
	original    config.Prompt
	err         error
}

type confirmationKind int

const (
	deleteConfirmation confirmationKind = iota + 1
	moveConfirmation
	duplicateConfirmation
)

type confirmation struct {
	kind   confirmationKind
	prompt config.Prompt
	err    error
}

func (item promptItem) FilterValue() string {
	return item.prompt.Name
}

type libraryScope int

const (
	allScope libraryScope = iota
	localScope
	globalScope
)

func (scope libraryScope) String() string {
	switch scope {
	case localScope:
		return "Local"
	case globalScope:
		return "Global"
	default:
		return "All"
	}
}

type promptDelegate struct{}

func (promptDelegate) Height() int  { return 3 }
func (promptDelegate) Spacing() int { return 1 }
func (promptDelegate) Update(tea.Msg, *list.Model) tea.Cmd {
	return nil
}

func (promptDelegate) Render(writer io.Writer, model list.Model, index int, raw list.Item) {
	item, ok := raw.(promptItem)
	if !ok {
		return
	}

	name := itemNameStyle.Render(item.prompt.Name)
	if index == model.Index() {
		name = selectedItemNameStyle.Render("> " + item.prompt.Name)
	} else {
		name = "  " + name
	}
	badge := globalBadgeStyle.Render("GLOBAL")
	if item.prompt.Source == config.SourceProject {
		badge = localBadgeStyle.Render("LOCAL")
	}

	lineWidth := max(1, model.Width()-2)
	firstLine := name
	if remaining := lineWidth - lipgloss.Width(name) - lipgloss.Width(badge) - 1; remaining >= 0 {
		firstLine += strings.Repeat(" ", remaining+1) + badge
	}
	fmt.Fprint(writer, firstLine)
	for _, excerpt := range promptExcerpt(item.prompt.Contents, max(1, lineWidth-2)) {
		fmt.Fprint(writer, "\n  "+itemDescriptionStyle.Render(excerpt))
	}
}

// Model is the Bubble Tea prompt picker model.
type Model struct {
	list           list.Model
	preview        viewport.Model
	prompts        []config.Prompt
	scope          libraryScope
	query          string
	searchFocused  bool
	helpOpen       bool
	scopeSelection map[libraryScope]config.Prompt
	configErr      error
	width          int
	height         int
	wide           bool
	selected       *config.Prompt
	cancelled      bool
	insert         func(config.Prompt) error
	insertErr      error
	libraries      LibraryMutator
	form           *promptForm
	confirmation   *confirmation
	operationErr   error
}

// New creates a picker from loaded configuration prompts. A load error is
// rendered as an actionable configuration state instead of replacing the TUI.
func New(prompts []config.Prompt, loadErrors ...error) Model {
	return newModel(prompts, nil, nil, loadErrors...)
}

// NewWithInsertion creates a picker that inserts prompts before it quits.
// Errors are retained in the picker so the user can correct and retry.
func NewWithInsertion(prompts []config.Prompt, insert func(config.Prompt) error, loadErrors ...error) Model {
	return newModel(prompts, insert, nil, loadErrors...)
}

// NewWithLibraries creates a picker with in-popup prompt management enabled.
func NewWithLibraries(prompts []config.Prompt, libraries LibraryMutator, loadErrors ...error) Model {
	return newModel(prompts, nil, libraries, loadErrors...)
}

// NewWithInsertionAndLibraries enables prompt insertion and management.
func NewWithInsertionAndLibraries(prompts []config.Prompt, insert func(config.Prompt) error, libraries LibraryMutator, loadErrors ...error) Model {
	return newModel(prompts, insert, libraries, loadErrors...)
}

func newModel(prompts []config.Prompt, insert func(config.Prompt) error, libraries LibraryMutator, loadErrors ...error) Model {
	var loadErr error
	if len(loadErrors) > 0 {
		loadErr = loadErrors[0]
	}

	promptList := list.New(nil, promptDelegate{}, 0, 0)
	promptList.Title = "Prompts"
	promptList.SetShowTitle(false)
	promptList.SetFilteringEnabled(false)
	promptList.SetShowFilter(false)
	promptList.SetShowHelp(false)
	promptList.SetShowStatusBar(false)
	promptList.SetShowPagination(false)
	promptList.DisableQuitKeybindings()

	preview := viewport.New(viewport.WithWidth(1), viewport.WithHeight(1))
	preview.FillHeight = true

	model := Model{
		list:           promptList,
		preview:        preview,
		prompts:        append([]config.Prompt(nil), prompts...),
		scopeSelection: make(map[libraryScope]config.Prompt),
		configErr:      loadErr,
		insert:         insert,
		libraries:      libraries,
	}
	model.refreshItems(config.Prompt{}, false)
	return model
}

func (model Model) Init() tea.Cmd { return nil }

// SelectedPrompt reports the prompt accepted by the user after SelectionMsg
// has been delivered back to the model.
func (model Model) SelectedPrompt() (config.Prompt, bool) {
	if model.selected == nil {
		return config.Prompt{}, false
	}
	return *model.selected, true
}

// Cancelled reports whether the picker was dismissed with Esc or q.
func (model Model) Cancelled() bool { return model.cancelled }

func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.resize(message.Width, message.Height)
		return model, nil
	case SelectionMsg:
		prompt := message.Prompt
		if model.insert != nil {
			return model, func() tea.Msg {
				return InsertionResultMsg{Prompt: prompt, Err: model.insert(prompt)}
			}
		}
		model.selected = &prompt
		return model, tea.Quit
	case InsertionResultMsg:
		if message.Err != nil {
			model.insertErr = message.Err
			return model, nil
		}
		prompt := message.Prompt
		model.selected = &prompt
		return model, tea.Quit
	case tea.KeyPressMsg:
		if model.helpOpen {
			if message.String() == "?" || message.String() == "esc" {
				model.helpOpen = false
			}
			return model, nil
		}
		if message.String() == "?" && !model.searchFocused && (model.confirmation != nil || model.form == nil || model.form.focus == 2) {
			model.helpOpen = true
			return model, nil
		}
		if model.confirmation != nil {
			return model.updateConfirmation(message)
		}
		if model.form != nil {
			return model.updateForm(message)
		}
		if model.operationErr != nil {
			if message.String() == "esc" {
				model.operationErr = nil
			}
			return model, nil
		}
		if model.searchFocused {
			switch message.String() {
			case "esc":
				model.query = ""
				model.searchFocused = false
				model.refreshItems(model.currentPrompt(), true)
				return model, nil
			case "enter", "tab":
				model.searchFocused = false
				return model, nil
			case "ctrl+c":
				model.cancelled = true
				return model, tea.Quit
			case "backspace":
				if model.query != "" {
					_, size := utf8.DecodeLastRuneInString(model.query)
					model.query = model.query[:len(model.query)-size]
					model.refreshItems(model.currentPrompt(), true)
				}
				return model, nil
			}
			text := message.Text
			if text == "" && message.Mod == 0 {
				key := message.String()
				if utf8.RuneCountInString(key) == 1 {
					text = key
				} else if message.Code == tea.KeySpace {
					text = " "
				}
			}
			if text != "" {
				model.query += text
				model.refreshItems(model.currentPrompt(), true)
			}
			return model, nil
		}
		switch message.String() {
		case "esc":
			model.cancelled = true
			return model, tea.Quit
		case "ctrl+c":
			model.cancelled = true
			return model, tea.Quit
		case "tab":
			model.setScope((model.scope + 1) % 3)
			return model, nil
		case "ctrl+a":
			model.setScope(allScope)
			return model, nil
		case "ctrl+l":
			model.setScope(localScope)
			return model, nil
		case "ctrl+g":
			model.setScope(globalScope)
			return model, nil
		case "/":
			model.searchFocused = true
			return model, nil
		case "enter":
			if item, ok := model.list.SelectedItem().(promptItem); ok && (model.configErr == nil || len(model.prompts) > 0) {
				prompt := item.prompt
				return model, func() tea.Msg { return SelectionMsg{Prompt: prompt} }
			}
			return model, nil
		case "a":
			if model.libraries != nil {
				model.openForm(createForm, config.Prompt{})
				return model, nil
			}
			break
		case "e":
			if model.libraries != nil {
				if prompt := model.currentPrompt(); prompt.Path != "" {
					model.openForm(editForm, prompt)
				}
				return model, nil
			}
			break
		case "alt+d":
			if model.libraries != nil {
				if prompt := model.currentPrompt(); prompt.Path != "" {
					model.confirmation = &confirmation{kind: deleteConfirmation, prompt: prompt}
					model.operationErr = nil
				}
				return model, nil
			}
			break
		case "d":
			if model.libraries != nil {
				if prompt := model.currentPrompt(); prompt.Path != "" {
					model.openForm(duplicateForm, prompt)
				}
				return model, nil
			}
			break
		case "m":
			if model.libraries != nil {
				if prompt := model.currentPrompt(); prompt.Path != "" {
					model.confirmation = &confirmation{kind: moveConfirmation, prompt: prompt}
					model.operationErr = nil
				}
				return model, nil
			}
			break
		case "up", "k":
			before := model.list.Index()
			model.list.CursorUp()
			if model.list.Index() != before {
				model.rememberSelection()
				model.refreshPreview()
			}
			return model, nil
		case "down", "j":
			before := model.list.Index()
			model.list.CursorDown()
			if model.list.Index() != before {
				model.rememberSelection()
				model.refreshPreview()
			}
			return model, nil
		case "pgup", "ctrl+u":
			model.preview.PageUp()
			return model, nil
		case "pgdown", "ctrl+d":
			model.preview.PageDown()
			return model, nil
		case "home":
			model.preview.GotoTop()
			return model, nil
		case "end":
			model.preview.GotoBottom()
			return model, nil
		}
	case tea.PasteMsg:
		if model.helpOpen || model.confirmation != nil {
			return model, nil
		}
		if model.form != nil {
			return model.updateForm(message)
		}
		if model.searchFocused && message.Content != "" {
			model.query += message.Content
			model.refreshItems(model.currentPrompt(), true)
			return model, nil
		}
	}

	var commands []tea.Cmd
	var command tea.Cmd
	model.list, command = model.list.Update(message)
	commands = append(commands, command)
	model.preview, command = model.preview.Update(message)
	commands = append(commands, command)
	return model, tea.Batch(commands...)
}

func (model *Model) openForm(mode formMode, prompt config.Prompt) {
	title := textinput.New()
	title.Prompt = ""
	title.Placeholder = "e.g. Review a change"
	body := textarea.New()
	body.Prompt = ""
	body.Placeholder = "Describe the task and the output you need"
	body.ShowLineNumbers = false

	destination := model.defaultDestination()
	if mode == editForm || mode == duplicateForm {
		title.SetValue(prompt.Name)
		body.SetValue(prompt.Contents)
		destination = prompt.Source
	}
	if mode == duplicateForm {
		title.SetValue(prompt.Name + " copy")
		title.CursorEnd()
	}
	form := &promptForm{
		mode:        mode,
		title:       title,
		body:        body,
		destination: destination,
		original:    prompt,
	}
	model.form = form
	model.sizeForm()
	model.operationErr = nil
	model.focusForm(0)
}

func (model Model) defaultDestination() string {
	switch model.scope {
	case globalScope:
		return config.SourceGlobal
	case localScope:
		return config.SourceProject
	}
	if prompt := model.currentPrompt(); prompt.Source != "" {
		return prompt.Source
	}
	return config.SourceProject
}

func (model *Model) focusForm(index int) tea.Cmd {
	form := model.form
	form.title.Blur()
	form.body.Blur()
	form.focus = index
	switch index {
	case 0:
		return form.title.Focus()
	case 1:
		return form.body.Focus()
	}
	return nil
}

func (model Model) updateForm(message tea.Msg) (tea.Model, tea.Cmd) {
	form := model.form
	if keyMessage, ok := message.(tea.KeyPressMsg); ok {
		switch keyMessage.String() {
		case "esc":
			model.form = nil
			return model, nil
		case "ctrl+s":
			model.saveForm()
			return model, nil
		case "tab":
			last := 2
			if form.mode == editForm {
				last = 1
			}
			return model, model.focusForm((form.focus + 1) % (last + 1))
		case "shift+tab":
			last := 2
			if form.mode == editForm {
				last = 1
			}
			next := form.focus - 1
			if next < 0 {
				next = last
			}
			return model, model.focusForm(next)
		case "enter":
			if form.focus == 0 {
				return model, model.focusForm(form.focus + 1)
			}
		case "left":
			if form.focus == 2 {
				form.destination = config.SourceProject
				form.err = nil
				return model, nil
			}
		case "right":
			if form.focus == 2 {
				form.destination = config.SourceGlobal
				form.err = nil
				return model, nil
			}
		case "space", " ":
			if form.focus == 2 {
				form.destination = otherSource(form.destination)
				form.err = nil
				return model, nil
			}
		}
	}

	var command tea.Cmd
	switch form.focus {
	case 0:
		form.title, command = form.title.Update(message)
	case 1:
		form.body, command = form.body.Update(message)
	}
	return model, command
}

func (model *Model) saveForm() {
	form := model.form
	if form.mode == duplicateForm {
		form.err = nil
		model.confirmation = &confirmation{kind: duplicateConfirmation, prompt: form.original}
		return
	}
	changes := config.Prompt{
		Name:     form.title.Value(),
		Contents: form.body.Value(),
	}
	if form.mode != createForm {
		changes.Description = form.original.Description
	}
	var (
		saved config.Prompt
		err   error
	)
	if form.mode == editForm {
		saved, err = model.libraries.Update(form.original, changes)
	} else {
		saved, err = model.libraries.Create(form.destination, changes)
	}
	if err != nil {
		form.err = err
		return
	}
	if form.mode == editForm {
		model.replacePrompt(form.original, saved)
	} else {
		model.prompts = append(model.prompts, saved)
	}
	model.form = nil
	model.revealPrompt(saved)
}

func (model *Model) revealPrompt(prompt config.Prompt) {
	if !model.inScope(prompt) {
		model.rememberSelection()
		if prompt.Source == config.SourceGlobal {
			model.scope = globalScope
		} else {
			model.scope = localScope
		}
	}
	if !promptMatchesQuery(prompt, model.query) {
		model.query = ""
	}
	model.refreshItems(prompt, true)
}

func (model Model) updateConfirmation(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	confirmation := model.confirmation
	switch message.String() {
	case "esc", "n":
		model.confirmation = nil
		return model, nil
	case "enter", "y":
		if confirmation.kind == deleteConfirmation {
			model.confirmDelete()
		} else if confirmation.kind == moveConfirmation {
			model.confirmMove()
		} else {
			model.confirmDuplicate()
		}
	}
	return model, nil
}

func (model *Model) confirmDuplicate() {
	form := model.form
	changes := config.Prompt{
		Name:        form.title.Value(),
		Description: form.original.Description,
		Contents:    form.body.Value(),
	}
	duplicated, err := model.libraries.Duplicate(form.original, form.destination, changes)
	if err != nil {
		model.confirmation.err = err
		return
	}
	model.prompts = append(model.prompts, duplicated)
	model.confirmation = nil
	model.form = nil
	model.revealPrompt(duplicated)
}

func (model *Model) confirmDelete() {
	prompt := model.confirmation.prompt
	target := model.neighborPrompt(prompt)
	if err := model.libraries.Delete(prompt); err != nil {
		model.confirmation.err = err
		return
	}
	model.removePrompt(prompt)
	model.confirmation = nil
	model.refreshItems(target, true)
}

func (model *Model) confirmMove() {
	prompt := model.confirmation.prompt
	destination := otherSource(prompt.Source)
	moved, err := model.libraries.Move(prompt, destination)
	if err != nil {
		var partial *config.PartialMoveError
		if errors.As(err, &partial) {
			if moved.Path != "" {
				model.prompts = append(model.prompts, moved)
			}
			model.confirmation = nil
			model.operationErr = fmt.Errorf("partial move: both files remain; source %q; destination %q: %w", partial.SourcePath, partial.DestinationPath, partial.Err)
			model.refreshItems(prompt, true)
			return
		}
		model.confirmation.err = err
		return
	}
	model.replacePrompt(prompt, moved)
	model.confirmation = nil
	model.refreshItems(moved, true)
}

func (model *Model) replacePrompt(old, replacement config.Prompt) {
	for index := range model.prompts {
		if samePrompt(model.prompts[index], old) {
			model.prompts[index] = replacement
			return
		}
	}
}

func (model *Model) removePrompt(prompt config.Prompt) {
	for index := range model.prompts {
		if samePrompt(model.prompts[index], prompt) {
			model.prompts = append(model.prompts[:index], model.prompts[index+1:]...)
			return
		}
	}
}

func (model Model) neighborPrompt(prompt config.Prompt) config.Prompt {
	items := model.list.Items()
	for index, raw := range items {
		item := raw.(promptItem)
		if !samePrompt(item.prompt, prompt) {
			continue
		}
		if index+1 < len(items) {
			return items[index+1].(promptItem).prompt
		}
		if index > 0 {
			return items[index-1].(promptItem).prompt
		}
	}
	return config.Prompt{}
}

func otherSource(source string) string {
	if source == config.SourceGlobal {
		return config.SourceProject
	}
	return config.SourceGlobal
}

func (model Model) View() tea.View {
	var body string
	switch {
	case model.helpOpen:
		body = model.hotkeyPanel()
	case model.confirmation != nil:
		body = model.confirmationPanel()
	case model.form != nil:
		body = model.formPanel()
	case model.configErr != nil && len(model.prompts) == 0:
		body = model.statePanel(errorStyle.Render("Could not load prompts") + "\n\n" + model.configErr.Error() + "\n\nFix the malformed Markdown prompt, then reopen the picker.")
	case len(model.list.Items()) == 0:
		body = model.statePanel(model.emptyState())
	default:
		listPanel := titledPanel("Prompts", model.list.View(), model.list.Width())
		previewPanel := model.previewPanel()
		if model.wide {
			body = lipgloss.JoinHorizontal(lipgloss.Top, listPanel, strings.Repeat(" ", pickerPanelGap), previewPanel)
		} else {
			body = lipgloss.JoinVertical(lipgloss.Left, listPanel, previewPanel)
		}
	}
	if model.configErr != nil && len(model.prompts) > 0 && model.form == nil && model.confirmation == nil {
		body = errorStyle.Render("Some prompts could not be loaded: ") + model.configErr.Error() + "\n" + body
	}
	if model.operationErr != nil {
		body = model.statePanel(errorStyle.Render("Prompt operation needs attention") + "\n\n" + model.operationErr.Error() + "\n\nPress Esc to acknowledge and return to the library.")
	}
	if model.insertErr != nil {
		body = model.statePanel(errorStyle.Render("Could not insert prompt")+"\n\n"+model.insertErr.Error()+"\n\nCheck that Herdr is running and the target pane still exists, then press Enter to retry.") + "\n" + body
	}

	footer := footerGroups([]footerGroup{{"/", "search"}, {"↑/↓", "navigate"}, {"tab", "scope"}, {"?", "shortcuts"}, {"enter", "select"}, {"esc", "close"}})
	if model.libraries != nil {
		footer = footerGroups([]footerGroup{{"/", "search"}, {"a", "add"}, {"e", "edit"}, {"d", "duplicate"}, {"m", strings.ToLower(model.moveActionLabel())}, {"alt+d", "delete"}, {"?", "help"}})
	} else if !model.wide {
		footer = footerGroups([]footerGroup{{"/", "search"}, {"arrows", "move"}, {"tab", "scope"}, {"^a/^l/^g", "views"}, {"?", "help"}, {"enter", "select"}})
	}
	footer = lipgloss.Wrap(footer, max(1, model.width), "")
	if model.form != nil || model.helpOpen {
		// The form includes its own shortcut help; omitting the global footer
		// leaves room for every field in short panes.
		footer = ""
	}
	header := pickerTitleStyle.Render("Prompt Library") + "  " + model.scopeTabs()
	search := helpStyle.Render("Search: ") + model.query
	if model.searchFocused {
		search += searchCursorStyle.Render(searchCursor)
	} else if model.query == "" {
		search += helpStyle.Render("/ to filter")
	}
	separator := "\n"
	if model.form == nil && !model.helpOpen && model.confirmation == nil {
		separator = "\n\n"
	}
	viewContents := header + separator + search + separator + body
	if footer != "" {
		viewContents += "\n" + footer
	}
	view := tea.NewView(outerStyle.Render(viewContents))
	view.AltScreen = true
	return view
}

func (model *Model) resize(width, height int) {
	model.width = max(1, width-outerStyle.GetHorizontalFrameSize())
	model.height = max(1, height-outerStyle.GetVerticalFrameSize())
	model.wide = width >= wideLayoutMinimum
	if model.form != nil {
		model.sizeForm()
	}

	bodyHeight := max(minimumPanelSize, model.height-chromeHeight)
	if model.wide {
		listWidth := max(30, (model.width-pickerPanelGap)*2/5)
		previewWidth := max(minimumPanelSize, model.width-pickerPanelGap-listWidth)
		model.setListSize(listWidth, bodyHeight)
		model.setPreviewSize(previewWidth, bodyHeight)
		return
	}

	listHeight := max(7, bodyHeight*2/5)
	if listHeight >= bodyHeight {
		listHeight = max(minimumPanelSize, bodyHeight/2)
	}
	previewHeight := max(minimumPanelSize, bodyHeight-listHeight)
	model.setListSize(model.width, listHeight)
	model.setPreviewSize(model.width, previewHeight)
}

func (model *Model) setListSize(panelWidth, panelHeight int) {
	model.list.SetSize(panelContentWidth(panelWidth), max(1, panelHeight-panelStyle.GetVerticalFrameSize()-panelInnerPadding))
}

func (model *Model) setPreviewSize(panelWidth, panelHeight int) {
	innerWidth := panelContentWidth(panelWidth)
	innerHeight := max(1, panelHeight-panelStyle.GetVerticalFrameSize()-panelInnerPadding)
	model.preview.SetWidth(innerWidth)
	model.preview.SetHeight(innerHeight)
	model.refreshPreview()
}

func (model *Model) refreshPreview() {
	item, ok := model.list.SelectedItem().(promptItem)
	if !ok {
		model.preview.SetContent("")
		return
	}
	content := item.prompt.Contents
	if model.preview.Width() > 1 {
		content = wrapPreview(content, model.preview.Width())
	}
	model.preview.SetContent(content)
	model.preview.GotoTop()
}

func wrapPreview(contents string, width int) string {
	width = max(1, width)
	physicalLines := strings.Split(strings.ReplaceAll(contents, "\r\n", "\n"), "\n")
	wrapped := make([]string, 0, len(physicalLines))
	for _, line := range physicalLines {
		wrapped = append(wrapped, wrapPreviewLine(line, width)...)
	}
	return strings.Join(wrapped, "\n")
}

func wrapPreviewLine(line string, width int) []string {
	if strings.TrimSpace(line) == "" {
		return []string{""}
	}

	var wrapped []string
	current := ""
	flush := func() {
		if current != "" {
			wrapped = append(wrapped, current)
			current = ""
		}
	}
	runes := []rune(line)
	for index := 0; index < len(runes); {
		start := index
		whitespace := unicode.IsSpace(runes[index])
		for index < len(runes) && unicode.IsSpace(runes[index]) == whitespace {
			index++
		}
		token := string(runes[start:index])
		if whitespace && current == "" && len(wrapped) > 0 {
			continue
		}
		if whitespace && current != "" && ansi.StringWidth(current+token) > width {
			flush()
			continue
		}
		if ansi.StringWidth(token) > width {
			trimmed := strings.TrimRightFunc(current, unicode.IsSpace)
			if trimmed != "" {
				current = trimmed
			}
			flush()
			for offset := 0; offset < ansi.StringWidth(token); offset += width {
				chunk := ansi.Cut(token, offset, offset+width)
				if chunk == "" {
					break
				}
				if ansi.StringWidth(chunk) == width || offset+ansi.StringWidth(chunk) < ansi.StringWidth(token) {
					wrapped = append(wrapped, chunk)
				} else {
					current = chunk
				}
			}
			continue
		}

		candidate := current + token
		if ansi.StringWidth(candidate) > width {
			flush()
		}
		current += token
	}
	flush()
	return wrapped
}

func (model *Model) setScope(scope libraryScope) {
	if scope == model.scope {
		return
	}
	model.rememberSelection()
	previous := model.currentPrompt()
	model.scope = scope
	target, ok := model.scopeSelection[scope]
	if !ok {
		target = previous
	}
	model.refreshItems(target, true)
}

func (model *Model) refreshItems(target config.Prompt, preserve bool) {
	type rankedItem struct {
		item  promptItem
		field int
		score int
		order int
	}

	query := strings.TrimSpace(model.query)
	ranked := make([]rankedItem, 0, len(model.prompts))
	for index, prompt := range model.prompts {
		if !model.inScope(prompt) {
			continue
		}
		item := rankedItem{item: promptItem{prompt: prompt}, order: index}
		if query != "" {
			var matched bool
			if item.score, matched = fuzzyScore(query, prompt.Name); matched {
				item.field = 2
			} else if item.score, matched = fuzzyScore(query, prompt.Contents); matched {
				item.field = 1
			} else {
				continue
			}
		}
		ranked = append(ranked, item)
	}
	if query != "" {
		sort.SliceStable(ranked, func(left, right int) bool {
			if ranked[left].field != ranked[right].field {
				return ranked[left].field > ranked[right].field
			}
			if ranked[left].score != ranked[right].score {
				return ranked[left].score > ranked[right].score
			}
			return ranked[left].order < ranked[right].order
		})
	}

	items := make([]list.Item, len(ranked))
	selectedIndex := 0
	for index, ranked := range ranked {
		items[index] = ranked.item
		if preserve && samePrompt(ranked.item.prompt, target) {
			selectedIndex = index
		}
	}
	model.list.SetItems(items)
	model.list.Select(selectedIndex)
	model.rememberSelection()
	model.refreshPreview()
}

func (model Model) inScope(prompt config.Prompt) bool {
	return model.scope == allScope ||
		(model.scope == localScope && prompt.Source == config.SourceProject) ||
		(model.scope == globalScope && prompt.Source == config.SourceGlobal)
}

func (model Model) currentPrompt() config.Prompt {
	item, _ := model.list.SelectedItem().(promptItem)
	return item.prompt
}

func (model *Model) rememberSelection() {
	if item, ok := model.list.SelectedItem().(promptItem); ok {
		model.scopeSelection[model.scope] = item.prompt
	}
}

func samePrompt(left, right config.Prompt) bool {
	if left.Path != "" || right.Path != "" {
		return left.Path != "" && left.Path == right.Path
	}
	return left.Name == right.Name && left.Description == right.Description &&
		left.Contents == right.Contents && left.Source == right.Source
}

func promptMatchesQuery(prompt config.Prompt, query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}
	for _, value := range []string{prompt.Name, prompt.Contents} {
		if _, matched := fuzzyScore(query, value); matched {
			return true
		}
	}
	return false
}

func fuzzyScore(query, candidate string) (int, bool) {
	needle := []rune(strings.ToLower(query))
	haystack := []rune(strings.ToLower(candidate))
	if len(needle) == 0 {
		return 0, true
	}

	score, queryIndex, previous := 0, 0, -2
	for index, character := range haystack {
		if character != needle[queryIndex] {
			continue
		}
		score += 10
		if index == previous+1 {
			score += 8
		}
		if index == 0 || unicode.IsSpace(haystack[index-1]) || strings.ContainsRune("-_/.", haystack[index-1]) {
			score += 5
		}
		if queryIndex == 0 {
			score -= index
		}
		previous = index
		queryIndex++
		if queryIndex == len(needle) {
			return score - len(haystack)/4, true
		}
	}
	return 0, false
}

// promptExcerpt provides a stable, compact preview even when a prompt starts
// with blank lines or contains long unbroken text.
func promptExcerpt(contents string, width int) []string {
	words := strings.Fields(contents)
	lines := make([]string, 0, 2)
	current := ""
	for _, word := range words {
		for ansi.StringWidth(word) > width {
			if current != "" {
				lines = append(lines, current)
				current = ""
				if len(lines) == 2 {
					return abbreviatedExcerpt(lines, width)
				}
			}
			lines = append(lines, ansi.Cut(word, 0, width))
			word = ansi.Cut(word, width, ansi.StringWidth(word))
			if len(lines) == 2 {
				return abbreviatedExcerpt(lines, width)
			}
		}
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if ansi.StringWidth(candidate) > width && current != "" {
			lines = append(lines, current)
			if len(lines) == 2 {
				return abbreviatedExcerpt(lines, width)
			}
			current = word
		} else {
			current = candidate
		}
	}
	if current != "" && len(lines) < 2 {
		lines = append(lines, current)
	}
	for len(lines) < 2 {
		lines = append(lines, "")
	}
	return lines
}

func abbreviatedExcerpt(lines []string, width int) []string {
	if width <= 3 {
		return lines
	}
	lines[len(lines)-1] = ansi.Cut(lines[len(lines)-1], 0, width-3) + "..."
	return lines
}

func (model Model) scopeTabs() string {
	parts := make([]string, 0, 3)
	for _, scope := range []libraryScope{allScope, localScope, globalScope} {
		style := scopeStyle
		label := scope.String()
		if scope == model.scope {
			style = activeScopeStyle
		}
		parts = append(parts, style.Render(label))
	}
	return strings.Join(parts, " ")
}

func (model Model) emptyState() string {
	if model.query != "" {
		return fmt.Sprintf("No prompts match %q in %s.\n\nPress / to edit the search, Esc while searching to clear it, or Tab to try another scope.", model.query, model.scope.String())
	}
	if len(model.prompts) == 0 {
		return "No prompts found.\n\nAdd prompts to the local or global library, then reopen the picker."
	}
	return fmt.Sprintf("No %s prompts found.\n\nPress Tab to switch scope or Ctrl+A to view all prompts.", strings.ToLower(model.scope.String()))
}

func (model Model) previewPanel() string {
	return titledPanel("Preview", model.preview.View(), model.preview.Width())
}

type footerGroup struct {
	key    string
	action string
}

func footerGroups(groups []footerGroup) string {
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		parts = append(parts, titleStyle.Render(group.key)+" "+helpStyle.Render(group.action))
	}
	return strings.Join(parts, "    ")
}

func titledPanel(title, contents string, innerWidth int) string {
	innerWidth = max(1, innerWidth)
	panelInnerWidth := innerWidth + panelInnerPadding*2
	label := " " + title + " "
	label = ansi.Cut(label, 0, max(1, panelInnerWidth-1))
	topFill := max(0, panelInnerWidth-ansi.StringWidth(label)-1)
	top := formFieldBorderStyle.Render("╭─") + previewTitleStyle.Render(label) + formFieldBorderStyle.Render(strings.Repeat("─", topFill)+"╮")

	rows := append([]string{strings.Repeat(" ", panelInnerWidth)}, strings.Split(contents, "\n")...)
	for index, row := range rows {
		if index > 0 {
			row = strings.Repeat(" ", panelInnerPadding) + ansi.Cut(row, 0, innerWidth)
		}
		row += strings.Repeat(" ", max(0, panelInnerWidth-ansi.StringWidth(row)))
		rows[index] = formFieldBorderStyle.Render("│") + row + formFieldBorderStyle.Render("│")
	}
	bottom := formFieldBorderStyle.Render("╰" + strings.Repeat("─", panelInnerWidth) + "╯")
	return top + "\n" + strings.Join(rows, "\n") + "\n" + bottom
}

func panelContentWidth(panelWidth int) int {
	return max(1, panelWidth-panelStyle.GetHorizontalFrameSize()-panelInnerPadding*2)
}

func (model Model) statePanel(contents string) string {
	width := max(1, model.width-panelStyle.GetHorizontalFrameSize())
	height := max(1, model.height-chromeHeight-panelStyle.GetVerticalFrameSize())
	return panelStyle.Width(width).Height(height).Render(lipgloss.Wrap(contents, width, ""))
}

func (model Model) formPanel() string {
	form := model.form
	heading := "Create Prompt"
	switch form.mode {
	case editForm:
		heading = "Edit Prompt"
	case duplicateForm:
		heading = "Duplicate Prompt"
	}
	destination := "Local"
	if form.destination == config.SourceGlobal {
		destination = "Global"
	}
	fieldWidth := model.formFieldWidth()
	lines := []string{formField("Title", form.title.View(), fieldWidth, form.focus == 0), "", formField("Prompt", form.body.View(), fieldWidth, form.focus == 1)}
	if form.mode != editForm {
		lines = append(lines, destinationControl(destination, form.focus == 2))
	}
	if form.err != nil {
		lines = append(lines, "", errorStyle.Render("Could not save prompt: ")+form.err.Error())
	}
	if !model.wide {
		lines = append(lines, helpStyle.Render("No Herdr placeholders | Tab fields | ^S save | Esc"))
	} else {
		lines = append(lines, "", helpStyle.Render(lipgloss.Wrap("Tab/Shift+Tab fields | Enter newline in prompt | arrows/space destination | Ctrl+S save | Esc cancel", fieldWidth, "")))
	}
	contents := strings.Join(lines, "\n")
	if model.wide {
		sidebarOuterWidth := max(18, model.formWidth()-fieldWidth-panelGap)
		sidebar := titledPanel("Herdr placeholders", helpStyle.Render("None exposed by Herdr 0.8.0."), panelContentWidth(sidebarOuterWidth))
		contents = lipgloss.JoinHorizontal(lipgloss.Top, contents, strings.Repeat(" ", panelGap), sidebar)
	}
	headingLine := lipgloss.PlaceHorizontal(model.formWidth(), lipgloss.Center, editorHeadingStyle.Render(heading))
	return panelStyle.Render("\n" + headingLine + "\n\n" + contents)
}

func (model *Model) sizeForm() {
	if model.form == nil {
		return
	}
	inputWidth := max(1, model.formFieldWidth()-2)
	model.form.title.SetWidth(inputWidth)
	model.form.body.SetWidth(inputWidth)
	model.form.body.SetHeight(max(3, model.height-17))
}

func (model Model) formWidth() int {
	width := model.width
	if width == 0 {
		width = 80
	}
	return max(3, width-panelStyle.GetHorizontalFrameSize())
}

func (model Model) formFieldWidth() int {
	width := model.formWidth()
	if model.wide {
		return max(3, (width-panelGap)*2/3)
	}
	return width
}

func formField(label, contents string, width int, focused bool) string {
	width = max(3, width)
	border := formFieldBorderStyle
	if focused {
		border = focusedFormFieldBorderStyle
	}
	labelText := formFieldLabelStyle.Render(" " + label + " ")
	topWidth := max(0, width-lipgloss.Width(labelText)-3)
	top := border.Render("╭─") + labelText + border.Render(strings.Repeat("─", topWidth)+"╮")
	innerWidth := width - 2
	contents = lipgloss.NewStyle().Width(innerWidth).MaxWidth(innerWidth).Render(contents)
	rows := strings.Split(contents, "\n")
	for index, row := range rows {
		rows[index] = border.Render("│") + row + border.Render("│")
	}
	bottom := border.Render("╰" + strings.Repeat("─", width-2) + "╯")
	return top + "\n" + strings.Join(rows, "\n") + "\n" + bottom
}

func destinationControl(destination string, focused bool) string {
	selected := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(accentColor).Padding(0, 1)
	unselected := lipgloss.NewStyle().Foreground(mutedColor).Padding(0, 1)
	local, global := unselected.Render("Local"), unselected.Render("Global")
	if destination == config.SourceGlobal {
		global = selected.Render("Global")
	} else {
		local = selected.Render("Local")
	}
	label := "Destination: "
	if focused {
		label = "> " + label
	} else {
		label = "  " + label
	}
	return formFieldLabelStyle.Render(label) + local + global
}

func (model Model) confirmationPanel() string {
	confirmation := model.confirmation
	prompt := confirmation.prompt
	scope := "Local"
	if prompt.Source == config.SourceGlobal {
		scope = "Global"
	}
	heading := "Delete prompt?"
	detail := fmt.Sprintf("Title: %s\nScope: %s\nPath: %s", prompt.Name, scope, prompt.Path)
	if confirmation.kind == moveConfirmation {
		destination := "Global"
		if prompt.Source == config.SourceGlobal {
			destination = "Local"
		}
		heading = "Move prompt?"
		detail += "\nDestination: " + destination
	} else if confirmation.kind == duplicateConfirmation {
		destination := "Local"
		if model.form.destination == config.SourceGlobal {
			destination = "Global"
		}
		heading = "Duplicate prompt?"
		detail = fmt.Sprintf("Source title: %s\nCopy title: %s\nDestination: %s", prompt.Name, model.form.title.Value(), destination)
	}
	if confirmation.err != nil {
		detail += "\n\n" + errorStyle.Render("Operation failed: ") + confirmation.err.Error()
	}
	return model.statePanel(titleStyle.Render(heading) + "\n\n" + detail + "\n\nEnter/y confirm | Esc/n cancel")
}

func (model Model) moveActionLabel() string {
	if model.currentPrompt().Source == config.SourceGlobal {
		return "Move to Local"
	}
	return "Move to Global"
}

func (model Model) hotkeyPanel() string {
	sections := []struct {
		title  string
		keys   string
		action string
	}{
		{"Navigation", "up/down, j/k, Tab", "select prompt; cycle scope"},
		{"Navigation", "Ctrl+A/Ctrl+L/Ctrl+G", "All/Local/Global scope"},
		{"Search", "/, Backspace", "focus search; edit query"},
		{"Search", "Enter/Tab, Esc", "finish search; clear query"},
		{"Prompt Actions", "a, e, d, m", "create, edit, duplicate, " + strings.ToLower(model.moveActionLabel())},
		{"Prompt Actions", "Alt+D, Enter", "delete with confirmation; insert"},
		{"Preview", "PgUp/PgDn, Ctrl+U/Ctrl+D", "scroll; Home/End jump"},
		{"Editor", "Tab, Shift+Tab, Ctrl+S, Esc", "fields; save; cancel"},
		{"Editor", "arrows/Space", "destination in create/duplicate"},
	}
	lines := []string{titleStyle.Render("Keyboard shortcuts"), helpStyle.Render("Press ? or Esc to close"), ""}
	lastTitle := ""
	for _, section := range sections {
		if section.title != lastTitle {
			lines = append(lines, titleStyle.Render(section.title))
			lastTitle = section.title
		}
		lines = append(lines, shortcutKeyStyle.Render(section.keys)+"  "+shortcutActionStyle.Render(section.action))
	}
	return model.statePanel(strings.Join(lines, "\n"))
}
