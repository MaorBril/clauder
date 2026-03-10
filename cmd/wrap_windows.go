//go:build windows

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var wrapCmd = &cobra.Command{
	Use:   "wrap [claude args...]",
	Short: "Run Claude Code with openclawder wrapper (not supported on Windows)",
	Long:  `The wrap command requires PTY support which is not available on Windows.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("the wrap command is not supported on Windows (requires PTY support)")
	},
}
