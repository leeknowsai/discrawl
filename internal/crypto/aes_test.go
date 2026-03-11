package crypto

import (
	"strings"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
		key       string
		wantErr   bool
	}{
		{
			name:      "basic encryption/decryption",
			plaintext: "hello world",
			key:       "my-secret-key-32-bytes-long-!!",
			wantErr:   false,
		},
		{
			name:      "empty plaintext",
			plaintext: "",
			key:       "my-secret-key",
			wantErr:   false,
		},
		{
			name:      "short key gets padded",
			plaintext: "test data",
			key:       "short",
			wantErr:   false,
		},
		{
			name:      "long key gets truncated",
			plaintext: "test data",
			key:       strings.Repeat("a", 64),
			wantErr:   false,
		},
		{
			name:      "unicode content",
			plaintext: "こんにちは世界 🌍",
			key:       "my-secret-key",
			wantErr:   false,
		},
		{
			name:      "long content",
			plaintext: strings.Repeat("Lorem ipsum dolor sit amet. ", 100),
			key:       "my-secret-key",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encrypt
			ciphertext, err := Encrypt(tt.plaintext, tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("Encrypt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Empty plaintext should return empty ciphertext
			if tt.plaintext == "" {
				if ciphertext != "" {
					t.Errorf("Encrypt('') should return '', got %q", ciphertext)
				}
				return
			}

			// Ciphertext should be different from plaintext
			if ciphertext == tt.plaintext {
				t.Error("Ciphertext should not equal plaintext")
			}

			// Decrypt
			decrypted, err := Decrypt(ciphertext, tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("Decrypt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Decrypted should match original
			if decrypted != tt.plaintext {
				t.Errorf("Decrypt() = %q, want %q", decrypted, tt.plaintext)
			}
		})
	}
}

func TestEncryptUniqueness(t *testing.T) {
	key := "test-key"
	plaintext := "hello"

	// Encrypt the same plaintext twice
	c1, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	c2, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// They should be different due to random nonce
	if c1 == c2 {
		t.Error("Encrypting the same plaintext twice should produce different ciphertexts")
	}

	// But both should decrypt to the same plaintext
	d1, _ := Decrypt(c1, key)
	d2, _ := Decrypt(c2, key)
	if d1 != plaintext || d2 != plaintext {
		t.Errorf("Both ciphertexts should decrypt to %q", plaintext)
	}
}

func TestDecryptInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		encoded   string
		key       string
		wantError bool
	}{
		{
			name:      "empty encoded string",
			encoded:   "",
			key:       "key",
			wantError: false, // Returns empty string, no error
		},
		{
			name:      "invalid base64",
			encoded:   "not-valid-base64!@#$",
			key:       "key",
			wantError: true,
		},
		{
			name:      "too short ciphertext",
			encoded:   "YWJj", // base64 for "abc", shorter than nonce
			key:       "key",
			wantError: true,
		},
		{
			name:      "corrupted ciphertext",
			encoded:   "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo=", // valid base64 but invalid ciphertext
			key:       "key",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decrypt(tt.encoded, tt.key)
			if (err != nil) != tt.wantError {
				t.Errorf("Decrypt() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestDecryptWrongKey(t *testing.T) {
	plaintext := "secret message"
	key1 := "correct-key"
	key2 := "wrong-key"

	ciphertext, err := Encrypt(plaintext, key1)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	// Try to decrypt with wrong key
	_, err = Decrypt(ciphertext, key2)
	if err == nil {
		t.Error("Decrypt() with wrong key should return error")
	}
}

func TestKeyPadding(t *testing.T) {
	plaintext := "test"

	// Test that different short keys are actually different after padding
	key1 := "a"
	key2 := "b"

	c1, _ := Encrypt(plaintext, key1)
	c2, _ := Encrypt(plaintext, key2)

	// Decrypt with correct keys
	d1, err1 := Decrypt(c1, key1)
	d2, err2 := Decrypt(c2, key2)

	if err1 != nil || err2 != nil {
		t.Fatal("Should decrypt with correct keys")
	}

	if d1 != plaintext || d2 != plaintext {
		t.Error("Should decrypt to original plaintext")
	}

	// Try cross-decryption (should fail)
	_, err := Decrypt(c1, key2)
	if err == nil {
		t.Error("Should not decrypt c1 with key2")
	}
}
