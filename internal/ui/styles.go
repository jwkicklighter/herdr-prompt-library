package ui

import "charm.land/lipgloss/v2"

var (
	// Herdr forwards its active terminal palette to pane applications. Using
	// ANSI cyan and gray keeps the picker aligned with that palette instead of
	// pinning it to fixed 256-color values.
	accentColor  = lipgloss.Color("6")
	mutedColor   = lipgloss.Color("7")
	excerptColor = lipgloss.Color("8")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentColor)
	pickerTitleStyle   = titleStyle.Underline(true)
	editorHeadingStyle = titleStyle.Copy().
				Bold(true).
				Reverse(true).
				Padding(0, 1)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mutedColor)
	formFieldBorderStyle = lipgloss.NewStyle().
				Foreground(mutedColor)
	focusedFormFieldBorderStyle = lipgloss.NewStyle().
					Foreground(accentColor)
	formFieldLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accentColor)
	previewTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accentColor)
	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor)
	searchCursorStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accentColor)
	shortcutKeyStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accentColor)
	shortcutActionStyle = helpStyle
	itemNameStyle       = lipgloss.NewStyle().
				Bold(true)
	selectedItemNameStyle = itemNameStyle.
				Foreground(accentColor)
	itemDescriptionStyle = lipgloss.NewStyle().
				Foreground(excerptColor)
	localBadgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentColor)
	globalBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accentColor)
	scopeStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(0, 1)
	activeScopeStyle = scopeStyle.
				Bold(true).
				Foreground(lipgloss.Color("0")).
				Background(accentColor)
	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("1"))
	outerStyle = lipgloss.NewStyle().Padding(outerPadding)
)
