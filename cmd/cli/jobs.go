package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dalemusser/mhsaudiotools/engine/job"
)

// runJobs lists the shared run history — the same file the desktop app's
// History card reads, so runs from either shell show up in both.
func runJobs(args []string) error {
	fs := flag.NewFlagSet("jobs", flag.ExitOnError)
	n := fs.Int("n", 20, "how many recent jobs to show")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "List recent generation runs, newest first.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	path, err := job.DefaultStorePath()
	if err != nil {
		return fmt.Errorf("no usable config directory: %w", err)
	}
	recs, err := job.OpenStore(path).List()
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		fmt.Println("no recorded jobs")
		return nil
	}
	for i, r := range recs {
		if i == *n {
			fmt.Printf("… and %d more\n", len(recs)-*n)
			break
		}
		fmt.Println(r.Summary())
	}
	return nil
}
