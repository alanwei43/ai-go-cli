package main

import (
	"os"

	"switch-hosts-cli/internal/cli"
)

func main() {
	app := cli.NewApp()
	rootCmd := app.NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
