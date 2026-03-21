package cmd

import (
	"fmt"

	"github.com/scryner/my-streamdeck/internal/app"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a config template",
	RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := app.WriteTemplate()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", path)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
