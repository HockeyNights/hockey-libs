package secret

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
)

const DefaultTokenBytes = 32

func Token(size int) (string, error) {
	if size <= 0 {
		size = DefaultTokenBytes
	}

	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("secret: read random: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func MustToken(size int) string {
	token, err := Token(size)
	if err != nil {
		panic(err)
	}
	return token
}

func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func Matches(token, hash string) bool {
	return Equal(Hash(token), hash)
}

const digits = "0123456789"

func NumericCode(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("secret: code length must be positive, got %d", length)
	}

	buf := make([]byte, length)
	max := big.NewInt(int64(len(digits)))

	for i := range buf {
		index, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("secret: read random: %w", err)
		}
		buf[i] = digits[index.Int64()]
	}

	return string(buf), nil
}
