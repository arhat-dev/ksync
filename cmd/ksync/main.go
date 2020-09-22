package main

import (
	"fmt"
	"os"

	"arhat.dev/ksync/pkg/cmd"
)

func main() {
	if err := cmd.NewKsyncCmd().Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to run ksync: %v", err)
	}
}
