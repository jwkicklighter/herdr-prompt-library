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

	"herdr-prompt-library/internal/config"
)

const (
	wideLayoutMinimum = 90
	panelGap          = 2
	chromeHeight      = 4
	minimumPanelSize  = 3
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
	prompt      config.Prompt
	description string
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
	description textinput.Model
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

func (promptDelegate) Height() int  { return 2 }
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
	description := "  " + itemDescriptionStyle.MaxWidth(max(1, lineWidth-2)).Render(item.description)
	fmt.Fprint(writer, firstLine+"\n"+description)
}

// Model is the Bubble Tea prompt picker model.
type Model struct {
	list           list.Model
	preview        viewport.Model
	prompts        []config.Prompt
	scope          libraryScope
	query          string
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
	promptList.SetFilteringEnabled(false)
	promptList.SetShowFilter(false)
	promptList.SetShowHelp(false)
	promptList.SetShowStatusBar(false)
	promptList.SetShowPagination(false)
	promptList.DisableQuitKeybindings()

	preview := viewport.New(viewport.WithWidth(1), viewport.WithHeight(1))
	preview.SoftWrap = true
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
		if model.form != nil {
			return model.updateForm(message)
		}
		if model.confirmation != nil {
			return model.updateConfirmation(message)
		}
		if model.operationErr != nil {
			if message.String() == "esc" {
				model.operationErr = nil
			}
			return model, nil
		}
		switch message.String() {
		case "esc":
			if model.query != "" {
				model.query = ""
				model.refreshItems(model.currentPrompt(), true)
				return model, nil
			}
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
		case "backspace":
			if model.query != "" {
				_, size := utf8.DecodeLastRuneInString(model.query)
				model.query = model.query[:len(model.query)-size]
				model.refreshItems(model.currentPrompt(), true)
			}
			return model, nil
		case "enter":
			if item, ok := model.list.SelectedItem().(promptItem); ok && (model.configErr == nil || len(model.prompts) > 0) {
				prompt := item.prompt
				return model, func() tea.Msg { return SelectionMsg{Prompt: prompt} }
			}
			return model, nil
		case "alt+a":
			if model.libraries != nil {
				model.openForm(createForm, config.Prompt{})
				return model, nil
			}
			break
		case "alt+e":
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
		case "alt+u":
			if model.libraries != nil {
				if prompt := model.currentPrompt(); prompt.Path != "" {
					model.openForm(duplicateForm, prompt)
				}
				return model, nil
			}
			break
		case "alt+m":
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
		if message.Text != "" {
			model.query += message.Text
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
	title.Placeholder = "Required title"
	description := textinput.New()
	description.Prompt = ""
	description.Placeholder = "Required description"
	body := textarea.New()
	body.Prompt = ""
	body.Placeholder = "Required prompt body"
	body.SetWidth(max(20, model.width-8))
	body.SetHeight(max(5, model.height-14))

	destination := model.defaultDestination()
	if mode == editForm || mode == duplicateForm {
		title.SetValue(prompt.Name)
		description.SetValue(prompt.Description)
		body.SetValue(prompt.Contents)
		destination = prompt.Source
	}
	if mode == duplicateForm {
		title.SetValue(prompt.Name + " copy")
	}
	form := &promptForm{
		mode:        mode,
		title:       title,
		description: description,
		body:        body,
		destination: destination,
		original:    prompt,
	}
	model.form = form
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
	form.description.Blur()
	form.body.Blur()
	form.focus = index
	switch index {
	case 0:
		return form.title.Focus()
	case 1:
		return form.description.Focus()
	case 2:
		return form.body.Focus()
	}
	return nil
}

func (model Model) updateForm(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	form := model.form
	switch message.String() {
	case "esc":
		model.form = nil
		return model, nil
	case "ctrl+s":
		model.saveForm()
		return model, nil
	case "tab":
		last := 3
		if form.mode == editForm {
			last = 2
		}
		return model, model.focusForm((form.focus + 1) % (last + 1))
	case "shift+tab":
		last := 3
		if form.mode == editForm {
			last = 2
		}
		next := form.focus - 1
		if next < 0 {
			next = last
		}
		return model, model.focusForm(next)
	case "enter":
		if form.focus < 2 {
			return model, model.focusForm(form.focus + 1)
		}
	case "left", "right", " ":
		if form.focus == 3 {
			form.destination = otherSource(form.destination)
			form.err = nil
			return model, nil
		}
	}

	var command tea.Cmd
	switch form.focus {
	case 0:
		form.title, command = form.title.Update(message)
	case 1:
		form.description, command = form.description.Update(message)
	case 2:
		form.body, command = form.body.Update(message)
	}
	return model, command
}

func (model *Model) saveForm() {
	form := model.form
	changes := config.Prompt{
		Name:        form.title.Value(),
		Description: form.description.Value(),
		Contents:    form.body.Value(),
	}
	var (
		saved config.Prompt
		err   error
	)
	if form.mode == editForm {
		saved, err = model.libraries.Update(form.original, changes)
	} else if form.mode == duplicateForm {
		saved, err = model.libraries.Duplicate(form.original, form.destination, changes)
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
		} else {
			model.confirmMove()
		}
	}
	return model, nil
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
	case model.form != nil:
		body = model.formPanel()
	case model.confirmation != nil:
		body = model.confirmationPanel()
	case model.configErr != nil && len(model.prompts) == 0:
		body = model.statePanel(errorStyle.Render("Could not load prompts") + "\n\n" + model.configErr.Error() + "\n\nFix the malformed Markdown prompt, then reopen the picker.")
	case len(model.list.Items()) == 0:
		body = model.statePanel(model.emptyState())
	default:
		listPanel := panelStyle.Render(model.list.View())
		previewPanel := model.previewPanel()
		if model.wide {
			body = lipgloss.JoinHorizontal(lipgloss.Top, listPanel, strings.Repeat(" ", panelGap), previewPanel)
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

	help := "type search | up/down navigate | tab scope | ctrl+a/ctrl+l/ctrl+g | enter select | esc clear/close"
	if model.libraries != nil {
		help = "type search | alt+a add | alt+e edit | alt+d delete | alt+u duplicate | alt+m move | enter insert"
	} else if !model.wide {
		help = "type search | arrows move | tab scope | ^a/^l/^g | enter select | esc clear/close"
	}
	footer := helpStyle.Render(help)
	header := titleStyle.Render("Prompt Library") + "  " + model.scopeTabs()
	search := helpStyle.Render("Search: ") + model.query
	if model.query == "" {
		search += helpStyle.Render("type to filter")
	}
	view := tea.NewView(header + "\n" + search + "\n" + body + "\n" + footer)
	view.AltScreen = true
	return view
}

func (model *Model) resize(width, height int) {
	model.width = max(1, width)
	model.height = max(1, height)
	model.wide = width >= wideLayoutMinimum
	if model.form != nil {
		model.form.body.SetWidth(max(20, model.width-8))
		model.form.body.SetHeight(max(5, model.height-14))
	}

	bodyHeight := max(minimumPanelSize, model.height-chromeHeight)
	if model.wide {
		listWidth := max(30, (model.width-panelGap)*2/5)
		previewWidth := max(minimumPanelSize, model.width-panelGap-listWidth)
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
	model.list.SetSize(max(1, panelWidth-panelStyle.GetHorizontalFrameSize()), max(1, panelHeight-panelStyle.GetVerticalFrameSize()))
}

func (model *Model) setPreviewSize(panelWidth, panelHeight int) {
	innerWidth := max(1, panelWidth-panelStyle.GetHorizontalFrameSize())
	innerHeight := max(1, panelHeight-panelStyle.GetVerticalFrameSize()-2)
	model.preview.SetWidth(innerWidth)
	model.preview.SetHeight(innerHeight)
}

func (model *Model) refreshPreview() {
	item, ok := model.list.SelectedItem().(promptItem)
	if !ok {
		model.preview.SetContent("")
		return
	}
	model.preview.SetContent(item.prompt.Contents)
	model.preview.GotoTop()
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
		item := rankedItem{item: promptItem{prompt: prompt, description: prompt.Description}, order: index}
		if query != "" {
			var matched bool
			if item.score, matched = fuzzyScore(query, prompt.Name); matched {
				item.field = 3
			} else if item.score, matched = fuzzyScore(query, prompt.Description); matched {
				item.field = 2
			} else if item.score, matched = fuzzyScore(query, prompt.Contents); matched {
				item.field = 1
				item.item.description = matchingExcerpt(prompt.Contents, query)
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
	for _, value := range []string{prompt.Name, prompt.Description, prompt.Contents} {
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

func matchingExcerpt(contents, query string) string {
	compact := strings.Join(strings.Fields(contents), " ")
	if compact == "" {
		return "Match in prompt body"
	}
	lowerContents := strings.ToLower(compact)
	lowerQuery := strings.ToLower(query)
	startByte := strings.Index(lowerContents, lowerQuery)
	if startByte < 0 {
		startByte = 0
		for _, character := range lowerQuery {
			if offset := strings.IndexRune(lowerContents[startByte:], character); offset >= 0 {
				startByte += offset
				break
			}
		}
	}
	const excerptLength = 72
	runes := []rune(compact)
	start := utf8.RuneCountInString(compact[:startByte])
	start = max(0, start-excerptLength/3)
	end := min(len(runes), start+excerptLength)
	excerpt := string(runes[start:end])
	if start > 0 {
		excerpt = "..." + excerpt
	}
	if end < len(runes) {
		excerpt += "..."
	}
	return excerpt
}

func (model Model) scopeTabs() string {
	parts := make([]string, 0, 3)
	for _, scope := range []libraryScope{allScope, localScope, globalScope} {
		style := scopeStyle
		label := scope.String()
		if scope == model.scope {
			style = activeScopeStyle
			label = "[" + label + "]"
		}
		parts = append(parts, style.Render(label))
	}
	return strings.Join(parts, " ")
}

func (model Model) emptyState() string {
	if model.query != "" {
		return fmt.Sprintf("No prompts match %q in %s.\n\nPress Backspace to edit the search, Esc to clear it, or Tab to try another scope.", model.query, model.scope.String())
	}
	if len(model.prompts) == 0 {
		return "No prompts found.\n\nAdd prompts to the local or global library, then reopen the picker."
	}
	return fmt.Sprintf("No %s prompts found.\n\nPress Tab to switch scope or Ctrl+A to view all prompts.", strings.ToLower(model.scope.String()))
}

func (model Model) previewPanel() string {
	item, _ := model.list.SelectedItem().(promptItem)
	heading := previewTitleStyle.Render("Preview")
	if item.prompt.Name != "" {
		heading += helpStyle.Render("  " + item.prompt.Name)
	}
	return panelStyle.Render(heading + "\n\n" + model.preview.View())
}

func (model Model) statePanel(contents string) string {
	width := max(1, model.width-panelStyle.GetHorizontalFrameSize())
	height := max(1, model.height-chromeHeight-panelStyle.GetVerticalFrameSize())
	return panelStyle.Width(width).Height(height).Render(lipgloss.Wrap(contents, width, ""))
}

func (model Model) formPanel() string {
	form := model.form
	heading := "Create prompt"
	switch form.mode {
	case editForm:
		heading = "Edit prompt"
	case duplicateForm:
		heading = "Duplicate prompt"
	}
	destination := "Local"
	if form.destination == config.SourceGlobal {
		destination = "Global"
	}
	lines := []string{
		titleStyle.Render(heading),
		"",
		formLabel("Title", form.focus == 0),
		form.title.View(),
		formLabel("Description", form.focus == 1),
		form.description.View(),
		formLabel("Body", form.focus == 2),
		form.body.View(),
	}
	if form.mode != editForm {
		lines = append(lines, formLabel("Destination", form.focus == 3), "  < Local | Global >  "+destination)
	}
	if form.err != nil {
		lines = append(lines, "", errorStyle.Render("Could not save prompt: ")+form.err.Error())
	}
	lines = append(lines, "", helpStyle.Render("Tab/Shift+Tab fields | Enter newline in body | arrows/space destination | Ctrl+S save | Esc cancel"))
	return panelStyle.Render(strings.Join(lines, "\n"))
}

func formLabel(label string, focused bool) string {
	if focused {
		return selectedItemNameStyle.Render("> " + label)
	}
	return itemNameStyle.Render("  " + label)
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
	}
	if confirmation.err != nil {
		detail += "\n\n" + errorStyle.Render("Operation failed: ") + confirmation.err.Error()
	}
	return model.statePanel(titleStyle.Render(heading) + "\n\n" + detail + "\n\nEnter/y confirm | Esc/n cancel")
}
