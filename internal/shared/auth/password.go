package auth

import (
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// MinAdminPasswordLength is the shortest admin dashboard password HashAdminPassword accepts.
const MinAdminPasswordLength = 12

// NormalizeAdminEmail is the admin_users.email form of an address (trimmed, lower case).
func NormalizeAdminEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// HashAdminPassword is the bcrypt hash admin_users.password_hash holds.
func HashAdminPassword(password string) (string, error) {
	if len(password) < MinAdminPasswordLength {
		return "", errors.New("admin password must be at least 12 characters")
	}
	if len(password) > 72 {
		return "", errors.New("admin password must be at most 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckAdminPassword reports whether password matches the admin_users.password_hash value.
func CheckAdminPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
