package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func TestPasswordHandler(t *testing.T) {
	t.Run("returns nil for empty password", func(t *testing.T) {
		handler := PasswordHandler("")
		if handler != nil {
			t.Error("expected nil handler for empty password")
		}
	})

	t.Run("validates correct password", func(t *testing.T) {
		handler := PasswordHandler("secret123")
		if handler == nil {
			t.Fatal("expected non-nil handler")
		}

		if !handler(nil, "secret123") {
			t.Error("expected true for correct password")
		}
	})

	t.Run("rejects incorrect password", func(t *testing.T) {
		handler := PasswordHandler("secret123")
		if handler == nil {
			t.Fatal("expected non-nil handler")
		}

		if handler(nil, "wrong") {
			t.Error("expected false for incorrect password")
		}
	})
}

func TestLoadAuthorizedKeysFromFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate a test key pair
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshPubKey, err := gossh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	authorizedKey := string(gossh.MarshalAuthorizedKey(sshPubKey))

	t.Run("loads valid authorized_keys file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "authorized_keys")
		content := "# Comment line\n" + authorizedKey + "\n# Another comment\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		keys, err := loadAuthorizedKeysFromFile(path)
		if err != nil {
			t.Fatalf("loadAuthorizedKeysFromFile failed: %v", err)
		}

		if len(keys) != 1 {
			t.Errorf("expected 1 key, got %d", len(keys))
		}
	})

	t.Run("skips invalid lines", func(t *testing.T) {
		path := filepath.Join(tmpDir, "authorized_keys2")
		content := "invalid line\n" + authorizedKey + "\nanother invalid\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		keys, err := loadAuthorizedKeysFromFile(path)
		if err != nil {
			t.Fatalf("loadAuthorizedKeysFromFile failed: %v", err)
		}

		if len(keys) != 1 {
			t.Errorf("expected 1 key (skipping invalid), got %d", len(keys))
		}
	})

	t.Run("handles empty file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "empty")
		if err := os.WriteFile(path, []byte(""), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		keys, err := loadAuthorizedKeysFromFile(path)
		if err != nil {
			t.Fatalf("loadAuthorizedKeysFromFile failed: %v", err)
		}

		if len(keys) != 0 {
			t.Errorf("expected 0 keys, got %d", len(keys))
		}
	})

	t.Run("returns error for nonexistent file", func(t *testing.T) {
		_, err := loadAuthorizedKeysFromFile("/nonexistent/file")
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})
}

func TestLoadAuthorizedKeysFromDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate test keys
	pubKey1, _, _ := ed25519.GenerateKey(rand.Reader)
	pubKey2, _, _ := ed25519.GenerateKey(rand.Reader)

	sshPubKey1, _ := gossh.NewPublicKey(pubKey1)
	sshPubKey2, _ := gossh.NewPublicKey(pubKey2)

	key1 := string(gossh.MarshalAuthorizedKey(sshPubKey1))
	key2 := string(gossh.MarshalAuthorizedKey(sshPubKey2))

	t.Run("loads keys from multiple files", func(t *testing.T) {
		dir := filepath.Join(tmpDir, "keys")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}

		os.WriteFile(filepath.Join(dir, "user1.pub"), []byte(key1), 0644)
		os.WriteFile(filepath.Join(dir, "user2.pub"), []byte(key2), 0644)

		keys, err := loadAuthorizedKeysFromDir(dir)
		if err != nil {
			t.Fatalf("loadAuthorizedKeysFromDir failed: %v", err)
		}

		if len(keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(keys))
		}
	})

	t.Run("skips hidden files", func(t *testing.T) {
		dir := filepath.Join(tmpDir, "keys2")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}

		os.WriteFile(filepath.Join(dir, "user.pub"), []byte(key1), 0644)
		os.WriteFile(filepath.Join(dir, ".hidden"), []byte(key2), 0644)

		keys, err := loadAuthorizedKeysFromDir(dir)
		if err != nil {
			t.Fatalf("loadAuthorizedKeysFromDir failed: %v", err)
		}

		if len(keys) != 1 {
			t.Errorf("expected 1 key (skipping hidden), got %d", len(keys))
		}
	})

	t.Run("skips subdirectories", func(t *testing.T) {
		dir := filepath.Join(tmpDir, "keys3")
		subDir := filepath.Join(dir, "subdir")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatalf("failed to create dirs: %v", err)
		}

		os.WriteFile(filepath.Join(dir, "user.pub"), []byte(key1), 0644)
		os.WriteFile(filepath.Join(subDir, "nested.pub"), []byte(key2), 0644)

		keys, err := loadAuthorizedKeysFromDir(dir)
		if err != nil {
			t.Fatalf("loadAuthorizedKeysFromDir failed: %v", err)
		}

		if len(keys) != 1 {
			t.Errorf("expected 1 key (not recursing), got %d", len(keys))
		}
	})
}

func TestLoadAuthorizedKeys(t *testing.T) {
	tmpDir := t.TempDir()

	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPubKey, _ := gossh.NewPublicKey(pubKey)
	key := string(gossh.MarshalAuthorizedKey(sshPubKey))

	t.Run("loads from file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "keys_file")
		os.WriteFile(path, []byte(key), 0644)

		keys, err := loadAuthorizedKeys(path)
		if err != nil {
			t.Fatalf("loadAuthorizedKeys failed: %v", err)
		}
		if len(keys) != 1 {
			t.Errorf("expected 1 key, got %d", len(keys))
		}
	})

	t.Run("loads from directory", func(t *testing.T) {
		dir := filepath.Join(tmpDir, "keys_dir")
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "key.pub"), []byte(key), 0644)

		keys, err := loadAuthorizedKeys(dir)
		if err != nil {
			t.Fatalf("loadAuthorizedKeys failed: %v", err)
		}
		if len(keys) != 1 {
			t.Errorf("expected 1 key, got %d", len(keys))
		}
	})
}

func TestGenerateAndLoadHostKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "host_key")

	t.Run("generates new host key", func(t *testing.T) {
		signer, err := generateHostKey(keyPath)
		if err != nil {
			t.Fatalf("generateHostKey failed: %v", err)
		}
		if signer == nil {
			t.Error("expected non-nil signer")
		}

		// Verify file was created with correct permissions
		info, err := os.Stat(keyPath)
		if err != nil {
			t.Fatalf("host key file not created: %v", err)
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
		}

		// Verify it's an ED25519 key
		if signer.PublicKey().Type() != "ssh-ed25519" {
			t.Errorf("expected ssh-ed25519 key, got %s", signer.PublicKey().Type())
		}
	})

	t.Run("loads existing host key", func(t *testing.T) {
		// Key was created in previous test
		signer, err := loadHostKey(keyPath)
		if err != nil {
			t.Fatalf("loadHostKey failed: %v", err)
		}
		if signer == nil {
			t.Error("expected non-nil signer")
		}
	})

	t.Run("returns error for invalid key file", func(t *testing.T) {
		invalidPath := filepath.Join(tmpDir, "invalid_key")
		os.WriteFile(invalidPath, []byte("not a valid key"), 0600)

		_, err := loadHostKey(invalidPath)
		if err == nil {
			t.Error("expected error for invalid key file")
		}
	})
}

func TestPublicKeyHandler(t *testing.T) {
	tmpDir := t.TempDir()

	// Generate a test key pair
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPubKey, _ := gossh.NewPublicKey(pubKey)
	authorizedKey := string(gossh.MarshalAuthorizedKey(sshPubKey))

	t.Run("returns nil for empty path", func(t *testing.T) {
		handler := PublicKeyHandler("")
		if handler != nil {
			t.Error("expected nil handler for empty path")
		}
	})

	t.Run("returns nil for nonexistent path", func(t *testing.T) {
		handler := PublicKeyHandler("/nonexistent/path")
		if handler != nil {
			t.Error("expected nil handler for nonexistent path")
		}
	})

	t.Run("returns nil for empty authorized_keys", func(t *testing.T) {
		path := filepath.Join(tmpDir, "empty_keys")
		os.WriteFile(path, []byte(""), 0644)

		handler := PublicKeyHandler(path)
		if handler != nil {
			t.Error("expected nil handler for empty authorized_keys")
		}
	})

	t.Run("validates authorized key", func(t *testing.T) {
		path := filepath.Join(tmpDir, "valid_keys")
		os.WriteFile(path, []byte(authorizedKey), 0644)

		handler := PublicKeyHandler(path)
		if handler == nil {
			t.Fatal("expected non-nil handler")
		}

		// Test with matching key
		if !handler(nil, sshPubKey) {
			t.Error("expected true for authorized key")
		}

		// Test with different key
		otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
		otherSSHPub, _ := gossh.NewPublicKey(otherPub)
		if handler(nil, otherSSHPub) {
			t.Error("expected false for unauthorized key")
		}
	})
}
