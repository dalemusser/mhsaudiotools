// Command mhsaudio is a thin terminal shell over the mhsaudiotools engine.
// It and the Wails app are both presentation layers; all behavior lives in
// engine/.
package main

import (
	"fmt"
	"os"
)

// version is set at build time via -ldflags "-X main.version=...", stamped from
// the git tag (see Makefile). It stays "dev" for un-stamped local builds.
var version = "dev"

const usage = `mhsaudio — generate game dialog audio with ElevenLabs

Usage:
  mhsaudio <command> [flags]

Commands:
  generate        Generate audio for a dialog source (use -dry-run to preview)
  scan            Find markup the cleanup profile misses; suggest remove rules
  voices          List the voices available on the account
  account         Show the account tier and its max concurrency
  import-voices   Convert/merge a VoiceAssignments.csv into a voices.json config
  version         Show the version

Run "mhsaudio <command> -h" for a command's flags.

The API key is read from $ELEVENLABS_API_KEY, or -key-file, or ~/.elevenlabs_key.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "generate":
		err = runGenerate(os.Args[2:])
	case "scan":
		err = runScan(os.Args[2:])
	case "voices":
		err = runVoices(os.Args[2:])
	case "account":
		err = runAccount(os.Args[2:])
	case "import-voices":
		err = runImportVoices(os.Args[2:])
	case "version", "--version":
		fmt.Printf("mhsaudio %s\nCopyright © 2026 Intelligence Builders. MIT License.\n", version)
		return
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
