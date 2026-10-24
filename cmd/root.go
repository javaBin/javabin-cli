package cmd

import (
	"fmt"
	"runtime/debug"
	"strings"

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
	// go install builds carry no ldflags; use the module version they were built from
	if version == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = strings.TrimPrefix(bi.Main.Version, "v")
		}
	}
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
