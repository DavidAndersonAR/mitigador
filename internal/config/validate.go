package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

// secretFieldSuffixes lists field-name substrings whose values must be redacted
// in validation error messages (T-01-03-05).
var secretFieldSuffixes = []string{"Token", "Password", "Secret", "SessionSecret"}

// isSecret returns true if the field name contains a secret suffix.
func isSecret(fieldName string) bool {
	for _, suffix := range secretFieldSuffixes {
		if strings.Contains(fieldName, suffix) {
			return true
		}
	}
	return false
}

// Validate runs struct-tag validation against the loaded config.
// Returns a multi-error joining all failing fields, formatted human-readably.
// Secret field values (Token, Password, Secret) are redacted from error messages.
func Validate(cfg *Config) error {
	if cfg == nil {
		return errors.New("config: nil")
	}
	v := validator.New(validator.WithRequiredStructEnabled())
	if err := v.Struct(cfg); err != nil {
		var verrs validator.ValidationErrors
		if errors.As(err, &verrs) {
			var sb strings.Builder
			sb.WriteString("config: validation failed:")
			for _, fe := range verrs {
				fieldValue := fmt.Sprint(fe.Value())
				if isSecret(fe.Field()) {
					fieldValue = "<redacted>"
				}
				fmt.Fprintf(&sb, "\n  - %s: failed %q (got %q)", fe.Namespace(), fe.Tag(), fieldValue)
			}
			return errors.New(sb.String())
		}
		return fmt.Errorf("config: validate: %w", err)
	}
	return nil
}
