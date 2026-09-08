package main

import (
	"fmt"
	"os"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("rosup %s (%s) %s\n", version, commit, date)
		return
	}
	fmt.Fprintln(os.Stderr, "rosup: CLI not implemented")
	os.Exit(2)
}
