package security

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func TestDefaultSensitiveFields(t *testing.T) {
	// Test that the slice is not empty
	assert.NotEmpty(t, DefaultSensitiveFields(), "DefaultSensitiveFields should not be empty")

	// Test that all expected fields are present
	expectedFields := []string{
		"password", "token", "secret", "key", "authorization",
		"auth", "credential", "credentials", "apikey", "api_key",
		"access_token", "accesstoken", "refresh_token", "refreshtoken", "private_key", "privatekey",
	}

	for _, expectedField := range expectedFields {
		assert.Contains(t, DefaultSensitiveFields(), expectedField,
			"DefaultSensitiveFields should contain %s", expectedField)
	}

	// Test that all fields are lowercase
	for _, field := range DefaultSensitiveFields() {
		assert.Equal(t, strings.ToLower(field), field,
			"All fields in DefaultSensitiveFields should be lowercase, but found: %s", field)
	}
}

func TestDefaultSensitiveFieldsMap(t *testing.T) {
	// Test that the map is not empty
	assert.NotEmpty(t, DefaultSensitiveFieldsMap(), "DefaultSensitiveFieldsMap should not be empty")

	// Test that map size matches slice size
	assert.Equal(t, len(DefaultSensitiveFields()), len(DefaultSensitiveFieldsMap()),
		"DefaultSensitiveFieldsMap should have the same number of entries as DefaultSensitiveFields")

	// Test that all slice entries are in the map
	for _, field := range DefaultSensitiveFields() {
		assert.True(t, DefaultSensitiveFieldsMap()[field],
			"DefaultSensitiveFieldsMap should contain field: %s", field)
	}

	// Test that all map entries are true
	for field, value := range DefaultSensitiveFieldsMap() {
		assert.True(t, value, "All values in DefaultSensitiveFieldsMap should be true, but %s is %v", field, value)
	}
}

func TestIsSensitiveField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		expected  bool
	}{
		{
			name:      "sensitive field - password",
			fieldName: "password",
			expected:  true,
		},
		{
			name:      "sensitive field - newpassword",
			fieldName: "newpassword",
			expected:  true,
		},
		{
			name:      "sensitive field - oldpassword",
			fieldName: "oldpassword",
			expected:  true,
		},
		{
			name:      "sensitive field - passwordsalt",
			fieldName: "passwordsalt",
			expected:  true,
		},
		{
			name:      "sensitive field - token",
			fieldName: "token",
			expected:  true,
		},
		{
			name:      "sensitive field - uppercase PASSWORD",
			fieldName: "PASSWORD",
			expected:  true,
		},
		{
			name:      "sensitive field - mixed case PaSsWoRd",
			fieldName: "PaSsWoRd",
			expected:  true,
		},
		{
			name:      "sensitive field - api_key",
			fieldName: "api_key",
			expected:  true,
		},
		{
			name:      "sensitive field - API_KEY uppercase",
			fieldName: "API_KEY",
			expected:  true,
		},
		{
			name:      "sensitive field - client_id",
			fieldName: "client_id",
			expected:  true,
		},
		{
			name:      "sensitive field - clientid",
			fieldName: "clientid",
			expected:  true,
		},
		{
			name:      "sensitive field - client_secret",
			fieldName: "client_secret",
			expected:  true,
		},
		{
			name:      "sensitive field - clientsecret",
			fieldName: "clientsecret",
			expected:  true,
		},

		{
			name:      "non-sensitive field - email",
			fieldName: "email",
			expected:  false,
		},
		{
			name:      "non-sensitive field - id",
			fieldName: "id",
			expected:  false,
		},
		{
			name:      "non-sensitive field - name",
			fieldName: "name",
			expected:  false,
		},
		{
			name:      "non-sensitive field - status",
			fieldName: "status",
			expected:  false,
		},
		{
			name:      "empty string",
			fieldName: "",
			expected:  false,
		},
		{
			name:      "partial match - pass (should not match password)",
			fieldName: "pass",
			expected:  false,
		},
		{
			name:      "partial match - word (should not match password)",
			fieldName: "word",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSensitiveField(tt.fieldName)
			assert.Equal(t, tt.expected, result,
				"IsSensitiveField(%s) should return %v", tt.fieldName, tt.expected)
		})
	}
}

func TestIsSensitiveFieldCaseInsensitive(t *testing.T) {
	// Test that case-insensitive matching works for all default fields
	for _, field := range DefaultSensitiveFields() {
		// Test lowercase
		assert.True(t, IsSensitiveField(field),
			"IsSensitiveField should return true for lowercase field: %s", field)

		// Test uppercase
		assert.True(t, IsSensitiveField(strings.ToUpper(field)),
			"IsSensitiveField should return true for uppercase field: %s", strings.ToUpper(field))

		// Test title case
		titleCaser := cases.Title(language.English)
		titleField := titleCaser.String(field)
		assert.True(t, IsSensitiveField(titleField),
			"IsSensitiveField should return true for title case field: %s", titleField)
	}
}

func TestConsistencyBetweenSliceAndMap(t *testing.T) {
	// Ensure that the slice and map are consistent
	// Every field in the slice should be in the map
	for _, field := range DefaultSensitiveFields() {
		assert.Contains(t, DefaultSensitiveFieldsMap(), field,
			"Field %s from DefaultSensitiveFields should exist in DefaultSensitiveFieldsMap", field)
		assert.True(t, DefaultSensitiveFieldsMap()[field],
			"Field %s in DefaultSensitiveFieldsMap should be true", field)
	}

	// Every field in the map should be in the slice
	for field := range DefaultSensitiveFieldsMap() {
		assert.Contains(t, DefaultSensitiveFields(), field,
			"Field %s from DefaultSensitiveFieldsMap should exist in DefaultSensitiveFields", field)
	}
}

func TestDefaultFieldsAreExpected(t *testing.T) {
	// Test that we have the expected number of fields (this helps catch accidental additions/removals)
	expectedCount := 59
	actualCount := len(DefaultSensitiveFields())
	assert.Equal(t, expectedCount, actualCount,
		"Expected %d default sensitive fields, but found %d. If this is intentional, update the test.",
		expectedCount, actualCount)
}

func TestNoEmptyFields(t *testing.T) {
	// Ensure no empty strings in the default fields
	for i, field := range DefaultSensitiveFields() {
		assert.NotEmpty(t, field, "Field at index %d should not be empty", i)
	}
}

func TestDefaultSensitiveFields_ReturnsClone(t *testing.T) {
	original := DefaultSensitiveFields()
	original[0] = "MUTATED"

	// The mutation should not affect subsequent calls
	fresh := DefaultSensitiveFields()
	assert.NotEqual(t, "MUTATED", fresh[0], "DefaultSensitiveFields must return a clone")
}

func TestIsSensitiveField_FinancialFields(t *testing.T) {
	financialFields := []struct {
		name     string
		expected bool
	}{
		{"card_number", true},
		{"cardnumber", true},
		{"cvv", true},
		{"cvc", true},
		{"ssn", true},
		{"social_security", true},
		{"pin", true},
		{"otp", true},
		{"account_number", true},
		{"accountnumber", true},
		{"routing_number", true},
		{"routingnumber", true},
		{"iban", true},
		{"swift", true},
		{"swift_code", true},
		{"bic", true},
		{"pan", true},
		{"expiry", true},
		{"expiry_date", true},
		{"expiration_date", true},
		{"card_expiry", true},
		{"date_of_birth", true},
		{"dob", true},
		{"tax_id", true},
		{"taxid", true},
		{"tin", true},
		{"national_id", true},
		{"sort_code", true},
		{"bsb", true},
		{"security_answer", true},
		{"security_question", true},
		{"mother_maiden_name", true},
		{"mfa_code", true},
		{"totp", true},
		{"biometric", true},
		{"fingerprint", true},
		// False positives for short tokens
		{"spinning", false},
		{"opinion", false},
		{"pineapple", false},
		{"cotton", false},
		{"panther", false},
	}

	for _, tt := range financialFields {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSensitiveField(tt.name)
			assert.Equal(t, tt.expected, result,
				"IsSensitiveField(%q) = %v, want %v", tt.name, result, tt.expected)
		})
	}
}

func TestShortSensitiveTokens_ExactMatch(t *testing.T) {
	// These short tokens should match exactly but not as substrings
	tests := []struct {
		field    string
		expected bool
	}{
		{"pin", true},
		{"otp", true},
		{"cvv", true},
		{"cvc", true},
		{"ssn", true},
		{"pan", true},
		{"bic", true},
		{"bsb", true},
		{"dob", true},
		{"tin", true},
		// CamelCase variants
		{"userPin", true},
		{"otpCode", true},
		{"userSsn", true},
		// Should NOT match as substrings in larger words
		{"spinning", false},
		{"option", false},
		{"panther", false},
		{"basic", false},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsSensitiveField(tt.field),
				"IsSensitiveField(%q)", tt.field)
		})
	}
}
