package main

import (
	"os"

	"github.com/aolmosj/azsel/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
