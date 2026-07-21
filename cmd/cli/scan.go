package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dalemusser/mhsaudiotools/engine/text"
)

func runScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	var (
		in          = fs.String("in", "", "dialog source file to scan (required)")
		adapter     = fs.String("source", "auto", "source format: auto, dbexport, simplescript")
		profilePath = fs.String("profile", "", "cleanup profile JSON (default: built-in mhs-dialogue)")
		noCleanup   = fs.Bool("no-cleanup", false, "scan raw text (show ALL markup, not just unhandled)")
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr,
			"Scan dialog for markup/stage-directions the cleanup profile doesn't remove,\n"+
				"and suggest remove rules to add. Makes no API calls.\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample:\n"+
			"  mhsaudio scan -in export.csv\n"+
			"  mhsaudio scan -in export.csv -profile cleanup.json\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		fs.Usage()
		return fmt.Errorf("-in is required")
	}

	lines, srcName, err := loadLines(*in, *adapter)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("no dialog lines found in %s", *in)
	}

	profile, err := loadProfile(*profilePath, *noCleanup)
	if err != nil {
		return err
	}

	texts := make([]string, len(lines))
	for i, li := range lines {
		texts[i] = li.Text
	}
	suggestions, err := text.Suggest(texts, profile)
	if err != nil {
		return err
	}

	// Header.
	fmt.Printf("Scanned %s lines (%s)\n", commas(len(lines)), srcName)
	if profile != nil {
		fmt.Printf("Through cleanup profile %q (%d rules)\n", profile.Name, len(profile.Rules))
	} else {
		fmt.Printf("Raw text — no cleanup applied\n")
	}

	if len(suggestions) == 0 {
		fmt.Printf("\nNo unhandled markup found — the profile covers everything scanned.\n")
		return nil
	}

	fmt.Printf("\n%d candidate pattern(s) the profile does NOT remove:\n", len(suggestions))
	for _, s := range suggestions {
		fmt.Printf("\n  %s — %s occurrence(s)\n", s.Description, commas(s.Count))
		fmt.Printf("    examples: %s\n", joinExamples(s.Examples, 8))
		fmt.Printf("    add as a remove rule (regex): %s\n", s.Pattern)
	}
	fmt.Printf("\nReview these — add the ones that are markup/directions to your cleanup.json.\n" +
		"(Parentheticals may be real speech; check before removing.)\n")
	return nil
}

func joinExamples(ex []string, max int) string {
	out := ""
	for i, e := range ex {
		if i == max {
			out += fmt.Sprintf("… (+%d more)", len(ex)-max)
			break
		}
		if i > 0 {
			out += "   "
		}
		out += e
	}
	return out
}
