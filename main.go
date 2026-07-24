package main

import (
	"errors"
	"os"

	"github.com/DPS0340/kubeoptimizer/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		if errors.Is(err, cmd.ErrWasteOverThreshold) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
