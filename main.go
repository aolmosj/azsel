package main

import (
	"os"

	"github.com/aolmosj/azsel/cmd"
)

var version = "dev"

func main() {
	cmd.Version = version
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
