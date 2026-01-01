package main

import (
	"os"

	"github.com/maorbril/clauder/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
