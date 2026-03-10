package main

import (
	"os"

	"github.com/orsi-bit/openclawder/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
