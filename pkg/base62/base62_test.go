package base62_test

import (
	"strings"
	"testing"

	"github.com/abdelrahmantarek/go-url-shortener/pkg/base62"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const charset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func TestGenerate_Length(t *testing.T) {
	code, err := base62.Generate()
	require.NoError(t, err)
	assert.Len(t, code, base62.DefaultLength)
}

func TestGenerate_Charset(t *testing.T) {
	for i := 0; i < 100; i++ {
		code, err := base62.Generate()
		require.NoError(t, err)
		for _, ch := range code {
			assert.True(t, strings.ContainsRune(charset, ch),
				"unexpected character %q in generated code %q", ch, code)
		}
	}
}

func TestGenerate_Uniqueness(t *testing.T) {
	const iterations = 1000
	seen := make(map[string]struct{}, iterations)
	for i := 0; i < iterations; i++ {
		code, err := base62.Generate()
		require.NoError(t, err)
		seen[code] = struct{}{}
	}
	// With 62^6 ≈ 56 billion possibilities, collisions in 1 000 attempts are astronomically rare.
	assert.Greater(t, len(seen), iterations-5, "too many collisions in generated codes")
}

func TestEncode_Zero(t *testing.T) {
	assert.Equal(t, "0", base62.Encode(0))
}

func TestEncode_KnownValues(t *testing.T) {
	assert.Equal(t, "1", base62.Encode(1))
	assert.Equal(t, "Z", base62.Encode(61))
	assert.Equal(t, "10", base62.Encode(62))
}

func TestEncode_NotEmpty(t *testing.T) {
	for _, n := range []uint64{1, 100, 99999, 1<<32 - 1} {
		assert.NotEmpty(t, base62.Encode(n))
	}
}

func TestGenerateN_CustomLength(t *testing.T) {
	for _, length := range []int{4, 8, 12} {
		code, err := base62.GenerateN(length)
		require.NoError(t, err)
		assert.Len(t, code, length)
	}
}
