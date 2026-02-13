//go:build unit

package jwt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var allAlgorithms = []string{AlgHS256, AlgHS384, AlgHS512}

func TestSign_Parse_RoundTrip_HS256(t *testing.T) {
	t.Parallel()

	claims := MapClaims{"sub": "user-1", "tenant_id": "abc"}
	secret := []byte("test-secret-256")

	tokenStr, err := Sign(claims, AlgHS256, secret)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenStr)

	token, err := Parse(tokenStr, secret, allAlgorithms)
	require.NoError(t, err)
	assert.True(t, token.SignatureValid)
	assert.Equal(t, "user-1", token.Claims["sub"])
	assert.Equal(t, "abc", token.Claims["tenant_id"])
	assert.Equal(t, "HS256", token.Header["alg"])
}

func TestSign_Parse_RoundTrip_HS384(t *testing.T) {
	t.Parallel()

	claims := MapClaims{"sub": "user-2"}
	secret := []byte("test-secret-384")

	tokenStr, err := Sign(claims, AlgHS384, secret)
	require.NoError(t, err)

	token, err := Parse(tokenStr, secret, allAlgorithms)
	require.NoError(t, err)
	assert.True(t, token.SignatureValid)
	assert.Equal(t, "user-2", token.Claims["sub"])
	assert.Equal(t, "HS384", token.Header["alg"])
}

func TestSign_Parse_RoundTrip_HS512(t *testing.T) {
	t.Parallel()

	claims := MapClaims{"sub": "user-3"}
	secret := []byte("test-secret-512")

	tokenStr, err := Sign(claims, AlgHS512, secret)
	require.NoError(t, err)

	token, err := Parse(tokenStr, secret, allAlgorithms)
	require.NoError(t, err)
	assert.True(t, token.SignatureValid)
	assert.Equal(t, "user-3", token.Claims["sub"])
	assert.Equal(t, "HS512", token.Header["alg"])
}

func TestParse_WrongSecret(t *testing.T) {
	t.Parallel()

	claims := MapClaims{"sub": "user-1"}
	secret := []byte("correct-secret")

	tokenStr, err := Sign(claims, AlgHS256, secret)
	require.NoError(t, err)

	_, err = Parse(tokenStr, []byte("wrong-secret"), allAlgorithms)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSignatureInvalid)
}

func TestParse_TamperedPayload(t *testing.T) {
	t.Parallel()

	claims := MapClaims{"sub": "user-1", "role": "user"}
	secret := []byte("test-secret")

	tokenStr, err := Sign(claims, AlgHS256, secret)
	require.NoError(t, err)

	parts := strings.Split(tokenStr, ".")
	tamperedPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"admin","role":"admin"}`))
	tampered := parts[0] + "." + tamperedPayload + "." + parts[2]

	_, err = Parse(tampered, secret, allAlgorithms)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSignatureInvalid)
}

func TestParse_AlgorithmNotAllowed(t *testing.T) {
	t.Parallel()

	claims := MapClaims{"sub": "user-1"}
	secret := []byte("test-secret")

	tokenStr, err := Sign(claims, AlgHS256, secret)
	require.NoError(t, err)

	_, err = Parse(tokenStr, secret, []string{AlgHS384, AlgHS512})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedAlgorithm)
}

func TestParse_NoneAlgorithmRejected(t *testing.T) {
	t.Parallel()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"attacker"}`))
	noneToken := header + "." + payload + "."

	_, err := Parse(noneToken, []byte("secret"), allAlgorithms)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedAlgorithm)
}

func TestParse_MalformedToken_WrongParts(t *testing.T) {
	t.Parallel()

	_, err := Parse("only.two", []byte("secret"), allAlgorithms)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidToken)

	_, err = Parse("one", []byte("secret"), allAlgorithms)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidToken)

	_, err = Parse("a.b.c.d", []byte("secret"), allAlgorithms)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestParse_EmptyToken(t *testing.T) {
	t.Parallel()

	_, err := Parse("", []byte("secret"), allAlgorithms)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestParse_ClaimsCorrectlyParsed(t *testing.T) {
	t.Parallel()

	claims := MapClaims{
		"tenant_id": "550e8400-e29b-41d4-a716-446655440000",
		"sub":       "user-42",
		"exp":       float64(9999999999),
	}
	secret := []byte("parse-claims-secret")

	tokenStr, err := Sign(claims, AlgHS256, secret)
	require.NoError(t, err)

	token, err := Parse(tokenStr, secret, allAlgorithms)
	require.NoError(t, err)
	assert.True(t, token.SignatureValid)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", token.Claims["tenant_id"])
	assert.Equal(t, "user-42", token.Claims["sub"])
	assert.InEpsilon(t, float64(9999999999), token.Claims["exp"], 0.001)
}

func TestParse_OversizedToken_ReturnsError(t *testing.T) {
	t.Parallel()

	// Build a token string that exceeds the 8192-byte maximum.
	oversized := strings.Repeat("a", 8193)

	_, err := Parse(oversized, []byte("secret"), allAlgorithms)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidToken)
	assert.Contains(t, err.Error(), "exceeds maximum length")
}

func TestSign_UnsupportedAlgorithm(t *testing.T) {
	t.Parallel()

	_, err := Sign(MapClaims{"sub": "x"}, "RS256", []byte("secret"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedAlgorithm)
}

func TestValidateTimeClaims_AllValid(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	claims := MapClaims{
		"exp": float64(now.Add(1 * time.Hour).Unix()),
		"nbf": float64(now.Add(-1 * time.Hour).Unix()),
		"iat": float64(now.Add(-30 * time.Minute).Unix()),
	}

	err := ValidateTimeClaimsAt(claims, now)
	assert.NoError(t, err)
}

func TestValidateTimeClaims_ExpiredToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	claims := MapClaims{
		"exp": float64(now.Add(-1 * time.Hour).Unix()),
	}

	err := ValidateTimeClaimsAt(claims, now)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestValidateTimeClaims_NotYetValid(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	claims := MapClaims{
		"nbf": float64(now.Add(1 * time.Hour).Unix()),
	}

	err := ValidateTimeClaimsAt(claims, now)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenNotYetValid)
}

func TestValidateTimeClaims_IssuedInFuture(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	claims := MapClaims{
		"iat": float64(now.Add(1 * time.Hour).Unix()),
	}

	err := ValidateTimeClaimsAt(claims, now)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenIssuedInFuture)
}

func TestValidateTimeClaims_MissingClaims(t *testing.T) {
	t.Parallel()

	err := ValidateTimeClaims(MapClaims{"sub": "user-1"})
	assert.NoError(t, err)
}

func TestValidateTimeClaims_EmptyClaims(t *testing.T) {
	t.Parallel()

	err := ValidateTimeClaims(MapClaims{})
	assert.NoError(t, err)
}

func TestValidateTimeClaims_WrongTypeSkipsGracefully(t *testing.T) {
	t.Parallel()

	claims := MapClaims{
		"exp": "not-a-number",
		"nbf": true,
		"iat": []string{"invalid"},
	}

	err := ValidateTimeClaims(claims)
	assert.NoError(t, err)
}

func TestValidateTimeClaims_JsonNumberFormat(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	future := now.Add(1 * time.Hour).Unix()
	past := now.Add(-1 * time.Hour).Unix()

	t.Run("valid json.Number claims", func(t *testing.T) {
		t.Parallel()

		claims := MapClaims{
			"exp": json.Number(fmt.Sprintf("%d", future)),
			"nbf": json.Number(fmt.Sprintf("%d", past)),
			"iat": json.Number(fmt.Sprintf("%d", past)),
		}

		err := ValidateTimeClaimsAt(claims, now)
		assert.NoError(t, err)
	})

	t.Run("expired json.Number", func(t *testing.T) {
		t.Parallel()

		claims := MapClaims{
			"exp": json.Number(fmt.Sprintf("%d", past)),
		}

		err := ValidateTimeClaimsAt(claims, now)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTokenExpired)
	})

	t.Run("invalid json.Number skips gracefully", func(t *testing.T) {
		t.Parallel()

		claims := MapClaims{
			"exp": json.Number("not-a-number"),
		}

		err := ValidateTimeClaimsAt(claims, now)
		assert.NoError(t, err)
	})
}

func TestValidateTimeClaims_BoundaryExpNow(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	// Token that expired 1 second ago — should be expired.
	claims := MapClaims{
		"exp": float64(now.Add(-1 * time.Second).Unix()),
	}

	err := ValidateTimeClaimsAt(claims, now)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestValidateTimeClaims_BoundaryNbfNow(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	// Token becomes valid 1 second ago — should be valid.
	claims := MapClaims{
		"nbf": float64(now.Add(-1 * time.Second).Unix()),
	}

	err := ValidateTimeClaimsAt(claims, now)
	assert.NoError(t, err)
}

func TestValidateTimeClaims_MultipleErrors_ReturnsFirst(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	// Both exp and nbf are invalid; exp is checked first.
	claims := MapClaims{
		"exp": float64(now.Add(-1 * time.Hour).Unix()),
		"nbf": float64(now.Add(1 * time.Hour).Unix()),
	}

	err := ValidateTimeClaimsAt(claims, now)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

func TestExtractTime_Float64(t *testing.T) {
	t.Parallel()

	ts := float64(1700000000)
	claims := MapClaims{"exp": ts}

	result, ok := extractTime(claims, "exp")
	assert.True(t, ok)
	assert.Equal(t, time.Unix(1700000000, 0).UTC(), result)
}

func TestExtractTime_JsonNumber(t *testing.T) {
	t.Parallel()

	claims := MapClaims{"exp": json.Number("1700000000")}

	result, ok := extractTime(claims, "exp")
	assert.True(t, ok)
	assert.Equal(t, time.Unix(1700000000, 0).UTC(), result)
}

func TestExtractTime_Missing(t *testing.T) {
	t.Parallel()

	claims := MapClaims{"sub": "user-1"}

	_, ok := extractTime(claims, "exp")
	assert.False(t, ok)
}

func TestExtractTime_InvalidType(t *testing.T) {
	t.Parallel()

	claims := MapClaims{"exp": "string-value"}

	_, ok := extractTime(claims, "exp")
	assert.False(t, ok)
}

func TestExtractTime_InvalidJsonNumber(t *testing.T) {
	t.Parallel()

	claims := MapClaims{"exp": json.Number("abc")}

	_, ok := extractTime(claims, "exp")
	assert.False(t, ok)
}
