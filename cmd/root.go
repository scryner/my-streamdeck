package cmd

import (
	"log"
	"os"

	"github.com/scryner/my-streamdeck/internal/app"
	"github.com/spf13/cobra"
)

var (
	enablePprof bool
	verboseMode bool
)

var rootCmd = &cobra.Command{
	Use:          "my-streamdeck",
	Short:        "Menu bar Elgato Stream Deck controller",
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		return app.RunMenuBar(app.RunOptions{
			EnablePprof: enablePprof,
			Verbose:     verboseMode,
		})
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&enablePprof, "pprof", false, "Enable pprof server on 127.0.0.1:6060")
	rootCmd.PersistentFlags().BoolVarP(&verboseMode, "verbose", "v", false, "Enable verbose lifecycle logs")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
