package keys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An explicitly named key file that can't be read must fail loudly — silently
// falling through to ~/.elevenlabs_key could spend the wrong account's credits.
func TestResolveExplicitFileFailsLoudly(t *testing.T) {
	t.Setenv(EnvVar, "")
	missing := filepath.Join(t.TempDir(), "nope.txt")
	_, err := Resolve(missing)
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("want error naming %s, got %v", missing, err)
	}
}

func TestResolveExplicitFileWins(t *testing.T) {
	t.Setenv(EnvVar, "")
	f := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(f, []byte("sk-test-123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	k, err := Resolve(f)
	if err != nil || k != "sk-test-123" {
		t.Fatalf("got %q, %v", k, err)
	}
}

func TestResolveEnvWins(t *testing.T) {
	t.Setenv(EnvVar, "sk-from-env")
	k, err := Resolve(filepath.Join(t.TempDir(), "ignored.txt"))
	if err != nil || k != "sk-from-env" {
		t.Fatalf("got %q, %v", k, err)
	}
}

// A KEY= line with an empty value in an explicit file is an error, not a
// silently empty key that 401s with no mention of the file.
func TestResolveExplicitFileEmptyValueErrors(t *testing.T) {
	t.Setenv(EnvVar, "")
	f := filepath.Join(t.TempDir(), "key.env")
	if err := os.WriteFile(f, []byte(EnvVar+"=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(f)
	if err == nil || !strings.Contains(err.Error(), f) {
		t.Fatalf("want error naming %s, got %v", f, err)
	}
}
