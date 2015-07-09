package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	shouldUpdate := flag.Bool("u", false, "update existing packages")
	flag.Parse()

	if err := save(flag.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "error adding new dependencies: %s", err.Error())
		os.Exit(1)
	}
	if *shouldUpdate {
		if err := update(flag.Args()); err != nil {
			fmt.Fprintf(os.Stderr, "error adding new dependencies: %s", err.Error())
			os.Exit(1)
		}
	}
}
