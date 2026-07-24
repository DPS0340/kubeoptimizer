package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/DPS0340/kubeoptimizer/internal/check"
	"github.com/DPS0340/kubeoptimizer/internal/cost"
	"github.com/DPS0340/kubeoptimizer/internal/report"
	"github.com/DPS0340/kubeoptimizer/internal/snapshot"
)

var (
	outputFormat string
	cpuRate      float64
	memRate      float64
	namespace    string
	failOver     float64
)

func validateOutput(f string) error {
	if f != "table" && f != "json" {
		return fmt.Errorf("invalid --output %q (want table or json)", f)
	}
	return nil
}

// ErrWasteOverThreshold marks the scripting contract: scan succeeded
// but waste exceeded --fail-over. Execute() maps it to exit code 2 so
// callers can tell "waste too high" from "scan failed" (exit 1).
var ErrWasteOverThreshold = errors.New("waste over threshold")

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan the cluster and report estimated monthly waste",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutput(outputFormat); err != nil {
			return err
		}
		if failOver < 0 {
			return fmt.Errorf("invalid --fail-over %v (must be >= 0)", failOver)
		}
		cfg, err := buildConfig()
		if err != nil {
			return fmt.Errorf("kubeconfig: %w", err)
		}
		kube, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return err
		}
		// Client construction never contacts the cluster; actual
		// availability is probed by Collect and reflected in the report.
		var mc metricsclient.Interface
		if m, err := metricsclient.NewForConfig(cfg); err == nil {
			mc = m
		}

		rates := cost.DefaultRates()
		if cpuRate > 0 {
			rates.CPUHourlyUSD = cpuRate
		}
		if memRate > 0 {
			rates.MemGBHourlyUSD = memRate
		}
		model := cost.NewModel(rates)

		snap := snapshot.Collect(cmd.Context(), kube, mc, namespace)
		var findings []check.Finding
		for _, c := range check.All() {
			findings = append(findings, c.Run(snap, model)...)
		}
		rep := report.Build(cfg.Host, snap, findings)
		if outputFormat == "json" {
			if err := report.RenderJSON(os.Stdout, rep); err != nil {
				return err
			}
		} else {
			report.RenderTable(os.Stdout, rep)
		}
		if cmd.Flags().Changed("fail-over") && rep.TotalMonthlyUSD > failOver {
			cmd.SilenceErrors = true // report already printed; no extra noise
			fmt.Fprintf(cmd.ErrOrStderr(), "fail-over: estimated waste $%.2f/mo exceeds threshold $%.2f/mo\n",
				rep.TotalMonthlyUSD, failOver)
			return ErrWasteOverThreshold
		}
		return nil
	},
}

func buildConfig() (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubecontext}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}

func init() {
	scanCmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "output format: table|json")
	scanCmd.Flags().Float64Var(&cpuRate, "cpu-rate", 0, "override $/vCPU-hour (for on-prem or custom pricing)")
	scanCmd.Flags().Float64Var(&memRate, "mem-rate", 0, "override $/GiB-hour")
	scanCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "limit scan to one namespace (cluster-scoped checks are skipped)")
	scanCmd.Flags().Float64Var(&failOver, "fail-over", 0, "exit 2 when estimated monthly waste exceeds this USD threshold")
	rootCmd.AddCommand(scanCmd)
}
