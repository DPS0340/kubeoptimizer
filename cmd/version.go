package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Populated at build time via -ldflags. Defaults cover `go install`.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "kubeoptimizer %s (commit %s, built %s, %s/%s)\n",
			version, commit, date, runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
