package command_test

import (
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/stretchr/testify/require"
)

func TestHelp(t *testing.T) {
	t.Parallel()

	for name, help := range command.Helps() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, help)
		})
	}
}
