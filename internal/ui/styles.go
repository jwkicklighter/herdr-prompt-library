package ui

import "charm.land/lipgloss/v2"

var (
	accentColor = lipgloss.Color("63")
	mutedColor  = lipgloss.Color("241")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentColor)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mutedColor)
	previewTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accentColor)
	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor)
	itemNameStyle = lipgloss.NewStyle().
			Bold(true)
	selectedItemNameStyle = itemNameStyle.
				Foreground(accentColor)
	itemDescriptionStyle = lipgloss.NewStyle().
				Foreground(mutedColor)
	localBadgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("42"))
	globalBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("39"))
	scopeStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(0, 1)
	activeScopeStyle = scopeStyle.
				Bold(true).
				Foreground(accentColor)
	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196"))
)
