package base62

import (
	"crypto/rand"
	"math/big"
)

const (
	charset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	base    = uint64(len(charset)) // 62
	DefaultLength = 6
)

// Encode turns a uint64 into a Base62 string.
func Encode(num uint64) string {
	if num == 0 {
		return string(charset[0])
	}
	buf := make([]byte, 0, 11)
	for num > 0 {
		buf = append(buf, charset[num%base])
		num /= base
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

// Generate returns a random 6-char Base62 code using crypto/rand.
func Generate() (string, error) {
	return GenerateN(DefaultLength)
}

func GenerateN(n int) (string, error) {
	buf := make([]byte, n)
	max := big.NewInt(int64(len(charset)))
	for i := range buf {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		buf[i] = charset[idx.Int64()]
	}
	return string(buf), nil
}
