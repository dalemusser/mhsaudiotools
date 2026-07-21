// Package keys resolves the ElevenLabs API key for whichever shell is running —
// the CLI or the desktop app. The key is never read from, or written to, source
// (see prior-apps/mhs dialogue 061026/VoiceLineGenerator.go for the anti-pattern
// this replaces: a live key committed on line 9).
package keys

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvVar is the environment variable checked first.
const EnvVar = "ELEVENLABS_API_KEY"

// HomeFile is the fallback key file in the user's home directory.
const HomeFile = ".elevenlabs_key"

// HomePath returns the path of the home-directory key file.
func HomePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, HomeFile), nil
}

// Resolve looks for the key in the environment first, then in each extra file
// given (in order), then in ~/.elevenlabs_key.
func Resolve(extraFiles ...string) (string, error) {
	if k := strings.TrimSpace(os.Getenv(EnvVar)); k != "" {
		return k, nil
	}
	candidates := make([]string, 0, len(extraFiles)+1)
	for _, f := range extraFiles {
		if f != "" {
			candidates = append(candidates, f)
		}
	}
	if home, err := HomePath(); err == nil {
		candidates = append(candidates, home)
	}
	for _, path := range candidates {
		if k, err := ReadFile(path); err == nil && k != "" {
			return k, nil
		}
	}
	return "", fmt.Errorf("no ElevenLabs API key found: set $%s, or save one to ~/%s", EnvVar, HomeFile)
}

// ReadFile accepts either a bare key or a KEY=VALUE line, so a secrets.env works
// as-is alongside a plain key file.
func ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if name, val, ok := strings.Cut(line, "="); ok {
			if strings.TrimSpace(name) == EnvVar {
				return strings.Trim(strings.TrimSpace(val), `"'`), nil
			}
			continue
		}
		return line, nil
	}
	return "", fmt.Errorf("no key found in %s", path)
}

// SaveToHome writes the key to ~/.elevenlabs_key with owner-only permissions and
// returns the path it wrote to.
//
// TODO (phase 2): store this in the OS keychain instead of a dotfile.
func SaveToHome(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("refusing to save an empty key")
	}
	path, err := HomePath()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
