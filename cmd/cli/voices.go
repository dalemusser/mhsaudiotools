package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/dalemusser/aigenaudiotools/engine/synth"
	"github.com/dalemusser/aigenaudiotools/engine/voice"
)

func runVoices(args []string) error {
	fs := flag.NewFlagSet("voices", flag.ExitOnError)
	keyFile := fs.String("key-file", "", "file holding the ElevenLabs API key")
	filter := fs.String("filter", "", "only show voices whose name contains this")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "List the voices available on the account.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	key, err := resolveAPIKey(*keyFile)
	if err != nil {
		return err
	}
	voices, err := synth.NewElevenLabs(key).Voices(context.Background())
	if err != nil {
		return err
	}
	sort.Slice(voices, func(i, j int) bool { return voices[i].Name < voices[j].Name })

	shown := 0
	for _, v := range voices {
		if *filter != "" && !strings.Contains(strings.ToLower(v.Name), strings.ToLower(*filter)) {
			continue
		}
		fmt.Printf("%-24s  %s\n", v.ID, v.Name)
		shown++
	}
	fmt.Printf("\n%d voices", shown)
	if *filter != "" {
		fmt.Printf(" matching %q (of %d)", *filter, len(voices))
	}
	fmt.Println()
	return nil
}

func runAccount(args []string) error {
	fs := flag.NewFlagSet("account", flag.ExitOnError)
	keyFile := fs.String("key-file", "", "file holding the ElevenLabs API key")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Show the account tier and its max concurrency.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	key, err := resolveAPIKey(*keyFile)
	if err != nil {
		return err
	}
	sub, err := synth.NewElevenLabs(key).Subscription(context.Background())
	if err != nil {
		return err
	}
	n, known := synth.MaxConcurrency(sub.Tier)

	fmt.Printf("Tier:        %s\n", sub.Tier)
	fmt.Printf("Characters:  %s used of %s\n", commas(sub.CharacterCount), commas(sub.CharacterLimit))
	if known {
		fmt.Printf("Parallel:    up to %d simultaneous requests\n", n)
	} else {
		fmt.Printf("Parallel:    tier not recognized — generate will use %d to be safe\n", n)
		fmt.Printf("             (pass -concurrency to override)\n")
	}
	return nil
}

func runImportVoices(args []string) error {
	fs := flag.NewFlagSet("import-voices", flag.ExitOnError)
	csvPath := fs.String("csv", "", "VoiceAssignments.csv to import (required)")
	outPath := fs.String("out", "voices.json", "voices config to create or update")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr,
			"Convert or merge a VoiceAssignments.csv into a voices.json config.\n\n"+
				"If -out already exists, existing player slot bindings are PRESERVED: a voice\n"+
				"keeps its Player<N> number no matter where it now sits in the CSV, and slots\n"+
				"missing from the CSV are kept rather than leaving a hole in the sequence.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *csvPath == "" {
		fs.Usage()
		return fmt.Errorf("-csv is required")
	}

	f, err := os.Open(*csvPath)
	if err != nil {
		return err
	}
	defer f.Close()
	imported, err := voice.LoadAssignmentsCSV(f)
	if err != nil {
		return err
	}

	cfg, err := voice.LoadConfig(*outPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// First import: the CSV's order establishes the slot numbering.
		cfg = imported
		fmt.Printf("Creating %s from %s\n", *outPath, *csvPath)
	} else {
		added := cfg.MergeFrom(imported)
		fmt.Printf("Updating %s from %s (existing slot bindings preserved)\n", *outPath, *csvPath)
		for _, s := range added {
			fmt.Printf("  + new player slot %d -> %s\n", s.Index, s.VoiceName)
		}
		if len(added) == 0 {
			fmt.Printf("  no new player voices\n")
		}
	}

	if err := cfg.Save(*outPath); err != nil {
		return err
	}

	fmt.Printf("\n%d characters:\n", len(cfg.Assignments))
	for _, a := range cfg.Assignments {
		fmt.Printf("  %-20s %s\n", a.Character, a.VoiceName)
	}
	fmt.Printf("\n%d player slots:\n", len(cfg.PlayerSlots))
	for _, s := range cfg.SortedSlots() {
		fmt.Printf("  Player%-2d             %s\n", s.Index, s.VoiceName)
	}
	for _, p := range cfg.Validate() {
		fmt.Fprintf(os.Stderr, "warning: %s\n", p)
	}
	return nil
}
