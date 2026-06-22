package tui

import (
	"fmt"

	"charm.land/huh/v2"
)

// Confirm asks a yes/no question, returning the user's choice. It requires a
// TTY (the caller should gate on terminal.Is); the default is no.
func Confirm(title, description string) (bool, error) {
	var ok bool
	field := huh.NewConfirm().Title(title).Description(description).Value(&ok)
	if err := huh.NewForm(huh.NewGroup(field)).WithTheme(Theme()).Run(); err != nil {
		return false, fmt.Errorf("confirm: %w", err)
	}
	return ok, nil
}
