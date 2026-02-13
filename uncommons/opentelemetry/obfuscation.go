package opentelemetry

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"

	cn "github.com/LerianStudio/lib-uncommons/uncommons/constants"
	"github.com/LerianStudio/lib-uncommons/uncommons/security"
)

// RedactionAction defines how sensitive values are transformed.
type RedactionAction string

const (
	RedactionMask RedactionAction = "mask"
	RedactionHash RedactionAction = "hash"
	RedactionDrop RedactionAction = "drop"
)

// RedactionRule matches fields/paths and applies a redaction action.
type RedactionRule struct {
	FieldPattern string
	PathPattern  string
	Action       RedactionAction

	fieldRegex *regexp.Regexp
	pathRegex  *regexp.Regexp
}

// Redactor applies ordered redaction rules to structured payloads.
type Redactor struct {
	rules     []RedactionRule
	maskValue string
}

// NewDefaultRedactor builds a mask-based redactor from default sensitive fields.
func NewDefaultRedactor() *Redactor {
	fields := security.DefaultSensitiveFields()

	rules := make([]RedactionRule, 0, len(fields))
	for _, field := range fields {
		rules = append(rules, RedactionRule{FieldPattern: `(?i)^` + regexp.QuoteMeta(field) + `$`, Action: RedactionMask})
	}

	r, err := NewRedactor(rules, cn.ObfuscatedValue)
	if err != nil {
		// WARNING: rule compilation failed; returning a minimal redactor that masks nothing.
		// This should never happen with default rules as they use QuoteMeta patterns.
		// If this occurs, investigate the DefaultSensitiveFields() patterns.
		return &Redactor{maskValue: cn.ObfuscatedValue}
	}

	return r
}

// NewRedactor compiles rules and returns a configured redactor.
func NewRedactor(rules []RedactionRule, maskValue string) (*Redactor, error) {
	if maskValue == "" {
		maskValue = cn.ObfuscatedValue
	}

	compiled := make([]RedactionRule, 0, len(rules))
	for i := range rules {
		rule := rules[i]
		if rule.Action == "" {
			rule.Action = RedactionMask
		}

		if rule.FieldPattern != "" {
			re, err := regexp.Compile(rule.FieldPattern)
			if err != nil {
				return nil, fmt.Errorf("invalid redaction field pattern at index %d: %w", i, err)
			}

			rule.fieldRegex = re
		}

		if rule.PathPattern != "" {
			re, err := regexp.Compile(rule.PathPattern)
			if err != nil {
				return nil, fmt.Errorf("invalid redaction path pattern at index %d: %w", i, err)
			}

			rule.pathRegex = re
		}

		compiled = append(compiled, rule)
	}

	return &Redactor{rules: compiled, maskValue: maskValue}, nil
}

func (r *Redactor) actionFor(path, fieldName string) (RedactionAction, bool) {
	if r == nil {
		return "", false
	}

	for i := range r.rules {
		rule := r.rules[i]
		pathMatch := true

		var fieldMatch bool
		if rule.fieldRegex != nil {
			fieldMatch = rule.fieldRegex.MatchString(fieldName)
		} else {
			fieldMatch = security.IsSensitiveField(fieldName)
		}

		if rule.pathRegex != nil {
			pathMatch = rule.pathRegex.MatchString(path)
		}

		if fieldMatch && pathMatch {
			return rule.Action, true
		}
	}

	return "", false
}

func (r *Redactor) redactValue(path, fieldName string, value any) (any, bool) {
	action, ok := r.actionFor(path, fieldName)
	if !ok {
		return value, false
	}

	switch action {
	case RedactionDrop:
		return nil, true
	case RedactionHash:
		return fmt.Sprintf("sha256:%x", hashString(fmt.Sprint(value))), false
	case RedactionMask:
		fallthrough
	default:
		return r.maskValue, false
	}
}

func hashString(v string) [32]byte {
	return sha256.Sum256([]byte(v))
}

// obfuscateStructFields recursively obfuscates sensitive fields in a struct or map.
func obfuscateStructFields(data any, path string, redactor *Redactor) any {
	switch v := data.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))

		for key, value := range v {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}

			if redactor != nil {
				redacted, drop := redactor.redactValue(childPath, key, value)
				if drop {
					continue
				}

				if !reflect.DeepEqual(redacted, value) {
					result[key] = redacted
					continue
				}
			}

			result[key] = obfuscateStructFields(value, childPath, redactor)
		}

		return result

	case []any:
		result := make([]any, len(v))

		for i, item := range v {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			result[i] = obfuscateStructFields(item, childPath, redactor)
		}

		return result

	default:
		return data
	}
}

// ObfuscateStruct applies obfuscation to a struct and returns the obfuscated data.
// This is a utility function that can be used independently of OpenTelemetry spans.
func ObfuscateStruct(valueStruct any, redactor *Redactor) (any, error) {
	if valueStruct == nil || redactor == nil {
		return valueStruct, nil
	}

	// Convert to JSON and back to get a generic representation.
	// Using any (not map[string]any) to handle arrays, primitives, and objects.
	jsonBytes, err := json.Marshal(valueStruct)
	if err != nil {
		return nil, err
	}

	var data any

	decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
	decoder.UseNumber()

	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}

	// Zero the intermediate buffer to minimize sensitive data lifetime in memory
	clear(jsonBytes)

	return obfuscateStructFields(data, "", redactor), nil
}
