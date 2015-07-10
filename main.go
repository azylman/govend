package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	updateExisting := flag.Bool("u", false, "update existing packages")
	noGet := flag.Bool("no-get", false, "do not run go get")
	flag.Parse()

	if !*noGet {
		args := append([]string{"get"}, os.Args[1:]...)
		if out, err := exec.Command("go", args...).Output(); err != nil {
			fmt.Fprintf(os.Stderr, "error running go get: %s, %s", err.Error(), out)
			os.Exit(1)
		}
	}

	if err := save(flag.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "error adding new dependencies: %s", err.Error())
		os.Exit(1)
	}
	if *updateExisting {
		if err := update(flag.Args()); err != nil {
			fmt.Fprintf(os.Stderr, "error adding new dependencies: %s", err.Error())
			os.Exit(1)
		}
	}
}
