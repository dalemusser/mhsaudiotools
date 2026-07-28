//go:build darwin

package keys

import (
	"fmt"
	"os/exec"
	"strings"
)

// macOS Keychain integration via the built-in /usr/bin/security tool — no
// dependencies, matching the engine's zero-dep rule. The item is a generic
// password under a fixed service/account pair.
//
// keychainService is a var so tests can point at a throwaway item instead of
// the real one.
var keychainService = "mhsaudio-elevenlabs"

const keychainAccount = "api-key"

// KeychainSupported reports whether this build can use a system keychain.
func KeychainSupported() bool { return true }

// keychainLookup returns the stored key, or ok=false when the item doesn't
// exist (or the keychain is unavailable — never an error, Resolve just falls
// through to the dotfile).
func keychainLookup() (string, bool) {
	out, err := exec.Command("/usr/bin/security",
		"find-generic-password", "-s", keychainService, "-a", keychainAccount, "-w").Output()
	if err != nil {
		return "", false // exit 44 = not found; anything else, fall through too
	}
	k := strings.TrimSpace(string(out))
	return k, k != ""
}

// SaveToKeychain stores (or replaces) the key in the user's login keychain.
// The secret is fed to `security -i` over stdin, never through argv, so it
// can't appear in a process listing.
func SaveToKeychain(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("refusing to save an empty key")
	}
	// The interactive parser splits on whitespace/quotes; real ElevenLabs keys
	// contain neither, so reject rather than invent escaping.
	if strings.ContainsAny(key, " \t\n\"'\\") {
		return fmt.Errorf("key contains characters that can't be stored safely — is it really an API key?")
	}

	cmd := exec.Command("/usr/bin/security", "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf(
		"add-generic-password -U -s %s -a %s -l %q -w %s\n",
		keychainService, keychainAccount, "ElevenLabs API key (mhsaudio)", key))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("storing in the keychain: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteFromKeychain removes the stored key; a missing item is not an error.
func DeleteFromKeychain() error {
	out, err := exec.Command("/usr/bin/security",
		"delete-generic-password", "-s", keychainService, "-a", keychainAccount).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "could not be found") {
			return nil
		}
		return fmt.Errorf("removing from the keychain: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
