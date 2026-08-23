package tui

import (
	"fmt"
	"strings"

	"github.com/CodeSyncr/nimbus/internal/ai"
	"github.com/charmbracelet/lipgloss"
)

func renderPlanView(m *Model) string {
	if m.Agent.Session == nil || m.Agent.Session.Plan == nil || len(m.Agent.Session.Plan.Steps) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#71717A")).Render("No architectural plan currently loaded.")
	}

	plan := m.Agent.Session.Plan
	width := m.Width - 4
	if width < 40 {
		width = 40
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3F3F46")).
		Padding(1, 2).
		MarginBottom(1).
		Width(width)

	headerTitle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#D97757")).
		Render("📋 ARCHITECTURAL IMPLEMENTATION PLAN")

	summaryLabel := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F4F4F5")).
		Render("🎯 Goal: ")

	summaryText := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D4D4D8")).
		Render(plan.Summary)

	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("#27272A")).Render(strings.Repeat("─", width-6))

	var sb strings.Builder
	sb.WriteString(headerTitle + "\n\n")
	sb.WriteString(summaryLabel + summaryText + "\n\n")

	if plan.Overview != "" {
		overviewLabel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#A1A1AA")).Render("📖 Architecture Overview:\n")
		overviewText := lipgloss.NewStyle().Foreground(lipgloss.Color("#D4D4D8")).Render(plan.Overview)
		sb.WriteString(overviewLabel + overviewText + "\n\n")
	}

	sb.WriteString(divider + "\n\n")

	// Group steps by Phase if phases are defined
	phaseMap := make(map[string][]ai.PlanStep)
	phaseOrder := make([]string, 0)

	if len(plan.Phases) > 0 {
		for _, p := range plan.Phases {
			phaseOrder = append(phaseOrder, p.Name)
			phaseMap[p.Name] = make([]ai.PlanStep, 0)
		}
	}

	// Place steps into phase buckets
	var unassignedSteps []ai.PlanStep
	for _, step := range plan.Steps {
		if step.Phase != "" {
			if _, exists := phaseMap[step.Phase]; !exists {
				phaseOrder = append(phaseOrder, step.Phase)
				phaseMap[step.Phase] = make([]ai.PlanStep, 0)
			}
			phaseMap[step.Phase] = append(phaseMap[step.Phase], step)
		} else {
			// Check if matched by file list in phases
			matched := false
			for _, p := range plan.Phases {
				for _, f := range p.Files {
					if strings.Contains(step.Target, f) || strings.Contains(f, step.Target) {
						phaseMap[p.Name] = append(phaseMap[p.Name], step)
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
			if !matched {
				unassignedSteps = append(unassignedSteps, step)
			}
		}
	}

	renderStepItem := func(step ai.PlanStep) string {
		actionColor := "#4ADE80"
		actionPrefix := "+ CREATE"
		switch strings.ToLower(step.Action) {
		case "create_file", "write_file", "create":
			actionColor = "#4ADE80"
			actionPrefix = "+ CREATE"
		case "edit_file", "edit":
			actionColor = "#D97757"
			actionPrefix = "~ EDIT  "
		case "run_command", "bash", "command":
			actionColor = "#38BDF8"
			actionPrefix = "⚡ EXEC  "
		case "delete_file", "delete":
			actionColor = "#F87171"
			actionPrefix = "- DELETE"
		}

		actionTag := lipgloss.NewStyle().
			Foreground(lipgloss.Color(actionColor)).
			Bold(true).
			Render(actionPrefix)

		targetStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F4F4F5")).
			Bold(true).
			Render(step.Target)

		riskTag := ""
		switch strings.ToLower(step.Risk) {
		case "medium", "med":
			riskTag = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Background(lipgloss.Color("#451A03")).Bold(true).Padding(0, 1).Render("MED RISK")
		case "high":
			riskTag = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")).Background(lipgloss.Color("#450A0A")).Bold(true).Padding(0, 1).Render("HIGH RISK")
		}

		descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
		descLine := fmt.Sprintf("          ↳ %s", descStyle.Render(step.Description))

		return fmt.Sprintf("    %s  %s%s\n%s\n", actionTag, targetStyle, riskTag, descLine)
	}

	// Render Phases & Steps
	if len(phaseOrder) > 0 {
		phasesHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F4F4F5")).Render("🚀 Phased Implementation Breakdown:\n\n")
		sb.WriteString(phasesHeader)

		for _, phaseName := range phaseOrder {
			pHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#38BDF8")).Render(fmt.Sprintf("✦ %s", phaseName))
			sb.WriteString(pHeader + "\n")

			// Find phase description if available
			for _, p := range plan.Phases {
				if p.Name == phaseName && p.Description != "" {
					pDesc := lipgloss.NewStyle().Foreground(lipgloss.Color("#71717A")).Render(fmt.Sprintf("  ↳ %s", p.Description))
					sb.WriteString(pDesc + "\n")
					break
				}
			}
			sb.WriteString("\n")

			steps := phaseMap[phaseName]
			if len(steps) > 0 {
				for _, step := range steps {
					sb.WriteString(renderStepItem(step) + "\n")
				}
			}
		}

		if len(unassignedSteps) > 0 {
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#38BDF8")).Render("✦ Additional Actions") + "\n\n")
			for _, step := range unassignedSteps {
				sb.WriteString(renderStepItem(step) + "\n")
			}
		}
	} else {
		// Fallback clean action list without clutter
		actionsHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F4F4F5")).Render("📄 Proposed Actions & File Changes:\n\n")
		sb.WriteString(actionsHeader)
		for _, step := range plan.Steps {
			sb.WriteString(renderStepItem(step) + "\n")
		}
	}

	// Key Architectural Highlights
	if len(plan.Details) > 0 {
		sb.WriteString(divider + "\n\n")
		detailsHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#A1A1AA")).Render("🔍 Key Architectural Highlights:\n")
		sb.WriteString(detailsHeader)
		for _, d := range plan.Details {
			sb.WriteString(fmt.Sprintf("  • %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Render(d)))
		}
		sb.WriteString("\n")
	}

	keyTag := func(k string, bg string) string {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color(bg)).
			Bold(true).
			Padding(0, 1).
			Render(k)
	}

	helpText := fmt.Sprintf(
		"%s Approve & Execute Plan        %s Reject / Cancel",
		keyTag("Enter", "#D97757"),
		keyTag("Esc", "#3F3F46"),
	)

	sb.WriteString(divider + "\n\n")
	sb.WriteString(helpText)

	return cardStyle.Render(sb.String())
}
