//go:build unit

package uncommons

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveAccents(t *testing.T) {
	t.Parallel()

	t.Run("accented", func(t *testing.T) {
		t.Parallel()

		result, err := RemoveAccents("café résumé")
		require.NoError(t, err)
		assert.Equal(t, "cafe resume", result)
	})

	t.Run("plain_text", func(t *testing.T) {
		t.Parallel()

		result, err := RemoveAccents("hello world")
		require.NoError(t, err)
		assert.Equal(t, "hello world", result)
	})
}

func TestRemoveSpaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"spaces", "a b c", "abc"},
		{"tabs", "a\tb\tc", "abc"},
		{"mixed", " a \t b \n c ", "abc"},
		{"empty", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, RemoveSpaces(tc.input))
		})
	}
}

func TestIsNilOrEmpty(t *testing.T) {
	t.Parallel()

	s := func(v string) *string { return &v }

	tests := []struct {
		name string
		val  *string
		want bool
	}{
		{"nil", nil, true},
		{"empty", s(""), true},
		{"whitespace", s("   "), true},
		{"null_string", s("null"), true},
		{"nil_string", s("nil"), true},
		{"valid", s("hello"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsNilOrEmpty(tc.val))
		})
	}
}

func TestCamelToSnakeCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "CamelCase", "camel_case"},
		{"lower", "already", "already"},
		{"multiple_upper", "HTTPServer", "h_t_t_p_server"},
		{"empty", "", ""},
		{"single_upper", "A", "a"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, CamelToSnakeCase(tc.input))
		})
	}
}

func TestRegexIgnoreAccents(t *testing.T) {
	t.Parallel()

	t.Run("accented_input", func(t *testing.T) {
		t.Parallel()

		result := RegexIgnoreAccents("café")
		assert.Contains(t, result, "[cç]")
		assert.Contains(t, result, "[aáàãâ]")
		assert.Contains(t, result, "[eéèê]")
	})

	t.Run("plain_input", func(t *testing.T) {
		t.Parallel()

		result := RegexIgnoreAccents("abc")
		assert.Contains(t, result, "[aáàãâ]")
		assert.Contains(t, result, "[cç]")
	})
}

func TestRemoveChars(t *testing.T) {
	t.Parallel()

	chars := map[string]bool{"-": true, ".": true}
	assert.Equal(t, "abc", RemoveChars("a-b.c", chars))
}

func TestReplaceUUIDWithPlaceholder(t *testing.T) {
	t.Parallel()

	path := "/api/v1/550e8400-e29b-41d4-a716-446655440000/items"
	assert.Equal(t, "/api/v1/:id/items", ReplaceUUIDWithPlaceholder(path))
}

func TestValidateServerAddress(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "localhost:8080", ValidateServerAddress("localhost:8080"))
	})

	t.Run("invalid_no_port", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", ValidateServerAddress("localhost"))
	})

	t.Run("invalid_empty", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", ValidateServerAddress(""))
	})
}

func TestHashSHA256(t *testing.T) {
	t.Parallel()

	h1 := HashSHA256("hello")
	h2 := HashSHA256("hello")

	assert.Equal(t, h1, h2)
	assert.Len(t, h1, 64) // SHA-256 hex is 64 chars
}

func TestStringToInt(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 42, StringToInt("42"))
	})

	t.Run("invalid_returns_100", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 100, StringToInt("not_a_number"))
	})
}
