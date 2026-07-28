package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dalemusser/mhsaudiotools/engine/keys"
)

// runKey shows where the API key resolves from, and optionally moves it into
// the macOS Keychain. It never prints the key itself.
func runKey(args []string) error {
	fs := flag.NewFlagSet("key", flag.ExitOnError)
	store := fs.Bool("store-keychain", false, "copy the resolved key into the macOS Keychain")
	rmFile := fs.Bool("rm-file", false, "with -store-keychain: also delete ~/.elevenlabs_key afterwards")
	keyFile := fs.String("key-file", "", "file holding the key (for -store-keychain)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Show where the ElevenLabs API key comes from; optionally store it in the macOS Keychain.\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n"+
			"  mhsaudio key                          # where does the key resolve from?\n"+
			"  mhsaudio key -store-keychain          # copy it into the Keychain\n"+
			"  mhsaudio key -store-keychain -rm-file # …and remove the plaintext dotfile\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !*store {
		if *rmFile {
			return fmt.Errorf("-rm-file only makes sense together with -store-keychain")
		}
		src, found := keys.Source(*keyFile)
		if !found {
			fmt.Printf("No API key found. Set $%s, save one to ~/%s, or use the app's key screen.\n",
				keys.EnvVar, keys.HomeFile)
			return nil
		}
		fmt.Printf("API key: %s\n", src)
		if keys.KeychainSupported() && src != "macOS Keychain" {
			fmt.Println("Tip: `mhsaudio key -store-keychain` moves it into the macOS Keychain.")
		}
		return nil
	}

	if !keys.KeychainSupported() {
		return fmt.Errorf("the system keychain is only supported on macOS")
	}
	key, err := keys.Resolve(*keyFile)
	if err != nil {
		return err
	}
	if err := keys.SaveToKeychain(key); err != nil {
		return err
	}
	fmt.Println("Key stored in the macOS Keychain.")

	if *rmFile {
		home, err := keys.HomePath()
		if err != nil {
			return err
		}
		if err := os.Remove(home); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("removing %s: %w", home, err)
		}
		fmt.Printf("Removed %s.\n", home)
	} else if home, err := keys.HomePath(); err == nil {
		if _, statErr := os.Stat(home); statErr == nil {
			fmt.Printf("Note: %s still exists (the Keychain now takes precedence); add -rm-file to remove it.\n", home)
		}
	}
	return nil
}
