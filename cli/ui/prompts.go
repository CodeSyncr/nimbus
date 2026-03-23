package ui

import survey "github.com/AlecAivazis/survey/v2"

// AskInput prompts for a single line of text input.
func (ui *UI) AskInput(label, def string) (string, error) {
	var ans string
	p := &survey.Input{Message: label, Default: def}
	err := survey.AskOne(p, &ans)
	return ans, err
}

// AskPassword prompts for a password (input hidden).
func (ui *UI) AskPassword(label string) (string, error) {
	var ans string
	p := &survey.Password{Message: label}
	err := survey.AskOne(p, &ans)
	return ans, err
}

// AskConfirm prompts for a yes/no confirmation.
func (ui *UI) AskConfirm(label string, def bool) (bool, error) {
	var ans bool
	p := &survey.Confirm{Message: label, Default: def}
	err := survey.AskOne(p, &ans)
	return ans, err
}

// AskSelect prompts for a single selection from a list.
func (ui *UI) AskSelect(label string, options []string, def string) (string, error) {
	var ans string
	p := &survey.Select{
		Message: label,
		Options: options,
		Default: def,
	}
	err := survey.AskOne(p, &ans)
	return ans, err
}

// AskMultiSelect prompts for a multi-selection (checkbox list).
func (ui *UI) AskMultiSelect(label string, options []string, defaults []string) ([]string, error) {
	var ans []string
	p := &survey.MultiSelect{
		Message: label,
		Options: options,
		Default: defaults,
	}
	err := survey.AskOne(p, &ans)
	return ans, err
}
