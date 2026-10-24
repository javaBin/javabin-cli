package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	jbaws "github.com/javaBin/javabin-cli/internal/aws"
	"github.com/javaBin/javabin-cli/internal/update"
)

// Set at build time by GoReleaser (-X .../cmd.version, cmd.commit).
var (
	version = "dev"
	commit  = "none"
)

var rootCmd = &cobra.Command{
	Use:   "javabin",
	Short: "Javabin platform CLI",
	Long:  "Developer CLI for the Javabin platform. Register apps, check status, and manage identity.",
}

func Execute() error {
	rootCmd.Version = version
	rootCmd.SetVersionTemplate(fmt.Sprintf("javabin {{.Version}} (%s)\n", commit))
	err := rootCmd.Execute()
	update.PrintIfOutdated(version)
	return err
}

func init() {
	rootCmd.PersistentFlags().StringVar(&jbaws.Profile, "profile", "", "AWS profile (default: AWS_PROFILE, or the SSO profile for javabin-team-<team> from app.yaml)")
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(registerCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(whoamiCmd)
}
