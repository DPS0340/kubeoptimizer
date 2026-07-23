package cmd

import (
	"github.com/spf13/cobra"
)

var (
	kubeconfig  string
	kubecontext string
)

var rootCmd = &cobra.Command{
	Use:   "kubeoptimizer",
	Short: "Read-only Kubernetes cost waste scanner",
	Long: "kubeoptimizer scans a cluster with get/list access only and reports\n" +
		"estimated monthly waste. It never mutates anything and never phones home.",
	SilenceUsage: true,
}

func Execute() error { return rootCmd.Execute() }

func init() {
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig (default: standard loading rules)")
	rootCmd.PersistentFlags().StringVar(&kubecontext, "context", "", "kubeconfig context to use")
}
