package monitor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const adminTokenFile = "admin.token"

// GenerateAdminToken creates a random 32-byte hex token, writes it to
// <dir>/admin.token, and returns the token string.
func GenerateAdminToken(dir string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate admin token: %w", err)
	}
	token := hex.EncodeToString(b)

	path := filepath.Join(dir, adminTokenFile)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("failed to write admin token file: %w", err)
	}
	return token, nil
}

// ReadAdminToken reads the admin token from <dir>/admin.token.
// Returns empty string if the file does not exist.
func ReadAdminToken(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, adminTokenFile))
	if err != nil {
		return ""
	}
	// Trim trailing newline.
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	return string(data)
}
