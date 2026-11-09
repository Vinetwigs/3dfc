// Copyright (C) 2026 Vinetwigs
// SPDX-License-Identifier: GPL-3.0-only

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	black = lipgloss.Color("0")
	red   = lipgloss.Color("196")
	white = lipgloss.Color("15")

	logoStyle          = lipgloss.NewStyle().Bold(true).Foreground(red)
	sectionStyle       = lipgloss.NewStyle().Bold(true).Foreground(red)
	titleStyle         = lipgloss.NewStyle().Bold(true).Foreground(white)
	dimStyle           = lipgloss.NewStyle().Foreground(white).Faint(true)
	menuLabelStyle     = lipgloss.NewStyle().Foreground(white)
	menuKeyStyle       = lipgloss.NewStyle().Foreground(red).Bold(true)
	menuSelectedStyle  = lipgloss.NewStyle().Background(red).Foreground(white).Bold(true)
	focusedRowStyle    = lipgloss.NewStyle().Background(red).Foreground(white).Bold(true)
	focusedInputStyle  = lipgloss.NewStyle().Background(red).Foreground(white)
	focusedPromptStyle = lipgloss.NewStyle().Background(red).Foreground(white).Bold(true)
	focusedHintStyle   = lipgloss.NewStyle().Background(red).Foreground(white)
	normalRowStyle     = lipgloss.NewStyle().Foreground(white)
	inactiveRowStyle   = lipgloss.NewStyle().Foreground(white).Faint(true)
	optionArrowStyle   = lipgloss.NewStyle().Foreground(red)
	optionValueStyle   = lipgloss.NewStyle().Foreground(white).Bold(true)
	percentStyle       = lipgloss.NewStyle().Foreground(red).Bold(true)
	metricLabelStyle   = lipgloss.NewStyle().Foreground(white).Faint(true).Bold(true)
	metricValueStyle   = lipgloss.NewStyle().Foreground(white)
	errStyle           = lipgloss.NewStyle().Foreground(red).Bold(true)
	warnStyle          = lipgloss.NewStyle().Foreground(red)
	resultSuccessTitle = lipgloss.NewStyle().Foreground(red).Bold(true)
	resultErrorTitle   = lipgloss.NewStyle().Foreground(red).Bold(true)
	resultLineStyle    = lipgloss.NewStyle().Foreground(white)
	resultSavedStyle   = lipgloss.NewStyle().Foreground(white).Bold(true)
	cursorStyle        = lipgloss.NewStyle().Foreground(red).Bold(true)

	keyStyle   = lipgloss.NewStyle().Background(red).Foreground(white).Padding(0, 1).Bold(true)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(red).
			Background(black).
			Foreground(white).
			Padding(1, 3)
)

func chip(key, desc string) string {
	return keyStyle.Render(key) + dimStyle.Render(" "+desc)
}

func hr(w int, c lipgloss.Color) string {
	if w < 4 {
		w = 4
	}
	return lipgloss.NewStyle().Foreground(c).Render(strings.Repeat("─", w))
}
