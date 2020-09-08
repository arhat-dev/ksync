package main

import (
	"fmt"
	"os"

	"arhat.dev/ksync/cmd/ksync/pkg"
)

func main() {
	if err := pkg.NewKsyncCmd().Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to run ksync: %v", err)
	}
}
