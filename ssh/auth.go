package ssh

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// AuthConfig holds SSH authentication configuration.
type AuthConfig struct {
	Password       string // Password for password authentication
	AuthorizedKeys string // Path to authorized_keys file
}

// PasswordHandler returns an ssh.PasswordHandler that validates against the configured password.
// Uses constant-time comparison to prevent timing attacks.
func PasswordHandler(password string) ssh.PasswordHandler {
	if password == "" {
		return nil
	}
	return func(ctx ssh.Context, pass string) bool {
		// Use constant-time comparison to prevent timing attacks
		return subtle.ConstantTimeCompare([]byte(pass), []byte(password)) == 1
	}
}

// PublicKeyHandler returns an ssh.PublicKeyHandler that validates against authorized_keys.
func PublicKeyHandler(authorizedKeysPath string) ssh.PublicKeyHandler {
	if authorizedKeysPath == "" {
		return nil
	}

	// Load authorized keys
	authorizedKeys, err := loadAuthorizedKeys(authorizedKeysPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load authorized keys from %s: %v\n", authorizedKeysPath, err)
		return nil
	}

	if len(authorizedKeys) == 0 {
		fmt.Fprintf(os.Stderr, "Warning: No authorized keys found in %s\n", authorizedKeysPath)
		return nil
	}

	return func(ctx ssh.Context, key ssh.PublicKey) bool {
		for _, authorizedKey := range authorizedKeys {
			if ssh.KeysEqual(key, authorizedKey) {
				return true
			}
		}
		return false
	}
}

// loadAuthorizedKeys loads public keys from an authorized_keys file or directory.
func loadAuthorizedKeys(path string) ([]ssh.PublicKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		return loadAuthorizedKeysFromDir(path)
	}
	return loadAuthorizedKeysFromFile(path)
}

// loadAuthorizedKeysFromFile loads public keys from a single authorized_keys file.
func loadAuthorizedKeysFromFile(path string) ([]ssh.PublicKey, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var keys []ssh.PublicKey
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, _, _, _, err := gossh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			// Skip invalid lines
			continue
		}
		keys = append(keys, key)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return keys, nil
}

// loadAuthorizedKeysFromDir loads public keys from all files in a directory.
func loadAuthorizedKeysFromDir(dir string) ([]ssh.PublicKey, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var keys []ssh.PublicKey
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// Skip files that don't look like keys
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		filePath := filepath.Join(dir, name)
		fileKeys, err := loadAuthorizedKeysFromFile(filePath)
		if err != nil {
			continue // Skip files that can't be read
		}
		keys = append(keys, fileKeys...)
	}

	return keys, nil
}

// GetOrCreateHostKey returns the host key signer, creating one if it doesn't exist.
// The key is stored at ~/.warlogix/host_key
func GetOrCreateHostKey() (gossh.Signer, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	warlogixDir := filepath.Join(homeDir, ".warlogix")
	keyPath := filepath.Join(warlogixDir, "host_key")

	// Check if key exists
	if _, err := os.Stat(keyPath); err == nil {
		return loadHostKey(keyPath)
	}

	// Create directory if needed
	if err := os.MkdirAll(warlogixDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate new ED25519 key
	return generateHostKey(keyPath)
}

// loadHostKey loads a host key from the specified path.
func loadHostKey(path string) (gossh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read host key: %w", err)
	}

	signer, err := gossh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse host key: %w", err)
	}

	return signer, nil
}

// generateHostKey generates a new ED25519 host key and saves it.
func generateHostKey(path string) (gossh.Signer, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	// Marshal to PEM format
	pemBlock, err := gossh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	pemData := pem.EncodeToMemory(pemBlock)

	// Write to file with secure permissions
	if err := os.WriteFile(path, pemData, 0600); err != nil {
		return nil, fmt.Errorf("failed to write host key: %w", err)
	}

	// Create signer
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	return signer, nil
}
