package hash

import (
	"golang.org/x/crypto/bcrypt"
)

// DefaultCost is the bcrypt cost used when not specified.
const DefaultCost = 10

// Make hashes the given password using bcrypt.
// Cost defaults to 10; use MakeWithCost for custom cost.
func Make(password string) (string, error) {
	return MakeWithCost(password, DefaultCost)
}

// MakeWithCost hashes the password with the given bcrypt cost (4-31).
func MakeWithCost(password string, cost int) (string, error) {
	if cost < bcrypt.MinCost {
		cost = bcrypt.MinCost
	}
	if cost > bcrypt.MaxCost {
		cost = bcrypt.MaxCost
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// Check verifies that the plaintext password matches the hash.
func Check(plaintext, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
	return err == nil
}
