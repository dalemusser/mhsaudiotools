//go:build !darwin

package keys

import "fmt"

// KeychainSupported reports whether this build can use a system keychain.
// Only macOS is supported today; Windows Credential Manager is a possible
// follow-up, and Linux keeps the dotfile (no universal secret service).
func KeychainSupported() bool { return false }

func keychainLookup() (string, bool) { return "", false }

// SaveToKeychain is unavailable off macOS.
func SaveToKeychain(string) error {
	return fmt.Errorf("the system keychain is only supported on macOS")
}

// DeleteFromKeychain is unavailable off macOS.
func DeleteFromKeychain() error {
	return fmt.Errorf("the system keychain is only supported on macOS")
}
