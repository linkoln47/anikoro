// Package crypto provides password hashing adapters for native accounts.
package crypto

import (
	"errors"

	"golang.org/x/crypto/bcrypt"

	"test/internal/ports"
)

// BcryptHasher implements ports.PasswordHasher using bcrypt. Callers are
// expected to reject passwords longer than 72 bytes before hashing, because
// bcrypt silently truncates beyond that length.
type BcryptHasher struct {
	cost int
}

var _ ports.PasswordHasher = (*BcryptHasher)(nil)

// NewBcryptHasher returns a hasher using the given cost. A non-positive or
// out-of-range cost falls back to bcrypt.DefaultCost.
func NewBcryptHasher(cost int) *BcryptHasher {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		cost = bcrypt.DefaultCost
	}
	return &BcryptHasher{cost: cost}
}

func (h *BcryptHasher) Hash(plainPassword string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(plainPassword), h.cost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func (h *BcryptHasher) Compare(hashedPassword, plainPassword string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(plainPassword))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		// Normalize the mismatch error so use cases can collapse it into a
		// generic invalid-credentials response.
		return ports.ErrPasswordMismatch
	}
	return err
}
