// Package ui implements the interactive prompt picker.
package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
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
}

// New creates a picker from loaded configuration prompts. A load error is
// rendered as an actionable configuration state instead of replacing the TUI.
func New(prompts []config.Prompt, loadErrors ...error) Model {
	return newModel(prompts, nil, loadErrors...)
}

// NewWithInsertion creates a picker that inserts prompts before it quits.
// Errors are retained in the picker so the user can correct and retry.
func NewWithInsertion(prompts []config.Prompt, insert func(config.Prompt) error, loadErrors ...error) Model {
	return newModel(prompts, insert, loadErrors...)
}

func newModel(prompts []config.Prompt, insert func(config.Prompt) error, loadErrors ...error) Model {
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
			if item, ok := model.list.SelectedItem().(promptItem); ok && model.configErr == nil {
				prompt := item.prompt
				return model, func() tea.Msg { return SelectionMsg{Prompt: prompt} }
			}
			return model, nil
		case "up":
			before := model.list.Index()
			model.list.CursorUp()
			if model.list.Index() != before {
				model.rememberSelection()
				model.refreshPreview()
			}
			return model, nil
		case "down":
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

func (model Model) View() tea.View {
	var body string
	switch {
	case model.configErr != nil:
		body = model.statePanel(errorStyle.Render("Could not load prompts") + "\n\n" + model.configErr.Error() + "\n\nFix .herdr/prompts.toml or $HERDR_PLUGIN_CONFIG_DIR/prompts.toml, then reopen the picker.")
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
	if model.insertErr != nil {
		body = model.statePanel(errorStyle.Render("Could not insert prompt")+"\n\n"+model.insertErr.Error()+"\n\nCheck that Herdr is running and the target pane still exists, then press Enter to retry.") + "\n" + body
	}

	help := "type search | up/down navigate | tab scope | ctrl+a/ctrl+l/ctrl+g | enter select | esc clear/close"
	if !model.wide {
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
	return left.Name == right.Name && left.Description == right.Description &&
		left.Contents == right.Contents && left.Source == right.Source
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
