package cmd

import (
	"log"
	"os"

	"github.com/scryner/my-streamdeck/internal/app"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:          "my-streamdeck",
	Short:        "Menu bar Elgato Stream Deck controller",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		return app.RunMenuBar()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
