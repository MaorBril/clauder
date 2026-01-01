package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of clauder",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("clauder version %s\n", Version)
		fmt.Printf("  Go version: %s\n", runtime.Version())
		fmt.Printf("  OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
