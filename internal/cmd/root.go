package cmd

import (
	"context"
	"time"

	"github.com/fcjr/sprout/internal/version"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "sprout",
	Short:   "sprout grows ISOs from docker compose files",
	Version: version.Version,
}

const commandTimeout = 15 * time.Second

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cobra.CheckErr(rootCmd.ExecuteContext(ctx))
}

func init() {
	rootCmd.SetVersionTemplate(version.String())

	// Root Flags
	rootCmd.Flags().BoolP("version", "v", false, "Get the version of sprout") // overrides default msg
}
