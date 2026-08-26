package tui

import (
	"fmt"
	"strings"

	"github.com/CodeSyncr/nimbus/internal/ai"
	"github.com/charmbracelet/lipgloss"
)

func renderPlanView(m *Model) string {
	if m.Agent == nil || m.Agent.Session == nil || m.Agent.Session.Plan == nil || len(m.Agent.Session.Plan.Steps) == 0 {
		return sMuted.Render("  No plan loaded.")
	}

	plan := m.Agent.Session.Plan
	width := contentWidth(m)
	inner := width - 6
	if inner < 34 {
		inner = 34
	}
	wrap := lipgloss.NewStyle().Width(inner)

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cAccent).
		Padding(1, 2).
		Width(width - 2)

	var sb strings.Builder

	// Title row: "Plan  Summary                          3 steps · 2 files · low risk"
	files := map[string]bool{}
	maxRisk := "low"
	for _, s := range plan.Steps {
		if s.Target != "" && !strings.EqualFold(s.Action, "run_command") {
			files[s.Target] = true
		}
		switch strings.ToLower(s.Risk) {
		case "high":
			maxRisk = "high"
		case "medium", "med":
			if maxRisk != "high" {
				maxRisk = "medium"
			}
		}
	}
	meta := sMuted.Render(fmt.Sprintf("%d steps · %d files · ", len(plan.Steps), len(files))) + riskStyle(maxRisk).Render(maxRisk+" risk")
	title := sAccentBold.Render("Plan") + "  " + sBold.Render(plan.Summary)
	sb.WriteString(wrap.Render(title) + "\n" + meta + "\n")

	if plan.Overview != "" && plan.Overview != plan.Summary {
		sb.WriteString("\n" + wrap.Inherit(sSoft).Render(plan.Overview) + "\n")
	}
	sb.WriteString("\n" + sDivider.Render(strings.Repeat("─", inner)) + "\n")

	// Group steps by phase, preserving plan order.
	type group struct {
		name, desc string
		steps      []ai.PlanStep
	}
	var groups []*group
	byName := map[string]*group{}
	for _, p := range plan.Phases {
		g := &group{name: p.Name, desc: p.Description}
		groups = append(groups, g)
		byName[p.Name] = g
	}
	var loose []ai.PlanStep
	for _, s := range plan.Steps {
		if s.Phase != "" {
			g, ok := byName[s.Phase]
			if !ok {
				g = &group{name: s.Phase}
				groups = append(groups, g)
				byName[s.Phase] = g
			}
			g.steps = append(g.steps, s)
			continue
		}
		loose = append(loose, s)
	}

	n := 0
	renderStep := func(s ai.PlanStep) {
		n++
		tag, color := "CREATE", cGreen
		switch strings.ToLower(s.Action) {
		case "edit_file", "edit":
			tag, color = "EDIT", cAccent
		case "run_command", "bash", "command":
			tag, color = "RUN", cPurple
		case "delete_file", "delete":
			tag, color = "DELETE", cRed
		}
		tagStyle := lipgloss.NewStyle().Foreground(color).Bold(true).Width(7)
		risk := ""
		switch strings.ToLower(s.Risk) {
		case "medium", "med":
			risk = "  " + riskStyle("medium").Render("medium risk")
		case "high":
			risk = "  " + riskStyle("high").Render("high risk")
		}
		sb.WriteString(fmt.Sprintf("  %s %s %s%s\n", sDim.Render(fmt.Sprintf("%2d", n)), tagStyle.Render(tag), sBold.Render(s.Target), risk))
		if s.Description != "" {
			desc := lipgloss.NewStyle().Width(inner - 13).Inherit(sMuted).Render(s.Description)
			sb.WriteString(indentLines("             "+glyphArrow+" "+desc, 0, false) + "\n")
		}
	}

	for _, g := range groups {
		if len(g.steps) == 0 {
			continue
		}
		sb.WriteString("\n" + sBlue.Bold(true).Render(g.name))
		if g.desc != "" {
			sb.WriteString("  " + sMuted.Render(g.desc))
		}
		sb.WriteString("\n")
		for _, s := range g.steps {
			renderStep(s)
		}
	}
	if len(loose) > 0 {
		if len(groups) > 0 {
			sb.WriteString("\n" + sBlue.Bold(true).Render("Other steps") + "\n")
		} else {
			sb.WriteString("\n")
		}
		for _, s := range loose {
			renderStep(s)
		}
	}

	if len(plan.Details) > 0 {
		sb.WriteString("\n" + sDivider.Render(strings.Repeat("─", inner)) + "\n")
		sb.WriteString(sMuted.Bold(true).Render("Notes") + "\n")
		for _, d := range plan.Details {
			sb.WriteString("  " + sAccent.Render("•") + " " + lipgloss.NewStyle().Width(inner-4).Inherit(sSoft).Render(d) + "\n")
		}
	}

	sb.WriteString("\n" + sDivider.Render(strings.Repeat("─", inner)) + "\n")
	sb.WriteString(sKey.Render("Enter") + sMuted.Render(" approve & run") + sDim.Render("     ") +
		sKey.Render("Esc") + sMuted.Render(" reject") + sDim.Render("     ") +
		sKey.Render("↑/↓") + sMuted.Render(" scroll"))

	return "\n" + card.Render(strings.TrimRight(sb.String(), "\n")) + "\n"
}

func riskStyle(level string) lipgloss.Style {
	switch level {
	case "high":
		return sRed.Bold(true)
	case "medium":
		return sYellow.Bold(true)
	}
	return sGreen
}
