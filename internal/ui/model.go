// Package ui implements the interactive prompt picker.
package ui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"herdr-prompt-library/internal/config"
)

const (
	wideLayoutMinimum = 90
	panelGap          = 2
	chromeHeight      = 3
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
	prompt config.Prompt
}

func (item promptItem) FilterValue() string {
	return item.prompt.Name + " " + item.prompt.Description + " " + item.prompt.Source
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
		badge = projectBadgeStyle.Render("PROJECT")
	}

	lineWidth := max(1, model.Width()-2)
	firstLine := name
	if remaining := lineWidth - lipgloss.Width(name) - lipgloss.Width(badge) - 1; remaining >= 0 {
		firstLine += strings.Repeat(" ", remaining+1) + badge
	}
	description := "  " + itemDescriptionStyle.MaxWidth(max(1, lineWidth-2)).Render(item.prompt.Description)
	fmt.Fprint(writer, firstLine+"\n"+description)
}

// Model is the Bubble Tea prompt picker model.
type Model struct {
	list      list.Model
	preview   viewport.Model
	configErr error
	width     int
	height    int
	wide      bool
	selected  *config.Prompt
	cancelled bool
	insert    func(config.Prompt) error
	insertErr error
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

	items := make([]list.Item, len(prompts))
	for index, prompt := range prompts {
		items[index] = promptItem{prompt: prompt}
	}

	promptList := list.New(items, promptDelegate{}, 0, 0)
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
		list:      promptList,
		preview:   preview,
		configErr: loadErr,
		insert:    insert,
	}
	model.refreshPreview()
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
		case "esc", "q":
			model.cancelled = true
			return model, tea.Quit
		case "enter":
			if item, ok := model.list.SelectedItem().(promptItem); ok && model.configErr == nil {
				prompt := item.prompt
				return model, func() tea.Msg { return SelectionMsg{Prompt: prompt} }
			}
			return model, nil
		case "up", "k":
			before := model.list.Index()
			model.list.CursorUp()
			if model.list.Index() != before {
				model.refreshPreview()
			}
			return model, nil
		case "down", "j":
			before := model.list.Index()
			model.list.CursorDown()
			if model.list.Index() != before {
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
		body = model.statePanel("No prompts found.\n\nAdd [[prompts]] entries to .herdr/prompts.toml or $HERDR_PLUGIN_CONFIG_DIR/prompts.toml, then reopen the picker.")
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

	help := "up/down or j/k navigate | pgup/pgdn scroll preview | enter select | esc/q cancel"
	if !model.wide {
		help = "j/k move | pgup/pgdn preview | enter select | q cancel"
	}
	footer := helpStyle.Render(help)
	view := tea.NewView(titleStyle.Render("Prompt Library") + "\n" + body + "\n" + footer)
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
