//go:build darwin

package keys

import (
	"fmt"
	"os"
	"testing"
)

// Exercises the real login keychain through a throwaway service name, removed
// when the test ends. Skipped in CI-like environments without a keychain.
func TestKeychainRoundTrip(t *testing.T) {
	orig := keychainService
	keychainService = fmt.Sprintf("mhsaudio-test-%d", os.Getpid())
	t.Cleanup(func() {
		_ = DeleteFromKeychain()
		keychainService = orig
	})

	if _, ok := keychainLookup(); ok {
		t.Fatal("throwaway service name unexpectedly present")
	}
	if err := SaveToKeychain("sk-test-keychain-123"); err != nil {
		t.Skipf("keychain unavailable in this environment: %v", err)
	}
	k, ok := keychainLookup()
	if !ok || k != "sk-test-keychain-123" {
		t.Fatalf("lookup = %q, %v; want stored key", k, ok)
	}

	// Overwrite (the -U flag) rather than erroring on an existing item.
	if err := SaveToKeychain("sk-test-keychain-456"); err != nil {
		t.Fatal(err)
	}
	if k, _ := keychainLookup(); k != "sk-test-keychain-456" {
		t.Fatalf("overwrite failed: %q", k)
	}

	if err := DeleteFromKeychain(); err != nil {
		t.Fatal(err)
	}
	if _, ok := keychainLookup(); ok {
		t.Fatal("item survived deletion")
	}
	// Deleting a missing item is a no-op, not an error.
	if err := DeleteFromKeychain(); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestSaveToKeychainRejectsUnsafeKeys(t *testing.T) {
	for _, bad := range []string{"", "has space", "has\"quote", "has\nnewline"} {
		if err := SaveToKeychain(bad); err == nil {
			t.Errorf("SaveToKeychain(%q) accepted an unsafe key", bad)
		}
	}
}
