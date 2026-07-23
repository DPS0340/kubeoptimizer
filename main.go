package main

import (
	"os"

	"github.com/DPS0340/kubeoptimizer/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
