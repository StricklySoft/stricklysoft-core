package config

import (
	"reflect"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// Validator is an optional interface that configuration structs may
// implement for custom validation logic. If the struct passed to
// [Loader.Load] implements Validator, its Validate method is called
// after tag-based validation ([required] tag) succeeds.
//
// Validate should return an error describing the first validation
// failure, or nil if the configuration is valid. Errors that are
// already [*sserr.Error] are returned as-is; other errors are wrapped
// with [sserr.CodeValidation].
//
// Example:
//
//	type DatabaseConfig struct {
//	    Host string `env:"HOST" required:"true"`
//	    Port int    `env:"PORT" required:"true"`
//	}
//
//	func (c *DatabaseConfig) Validate() error {
//	    if c.Port < 1 || c.Port > 65535 {
//	        return sserr.Newf(sserr.CodeValidation,
//	            "config: port %d is out of range [1, 65535]", c.Port)
//	    }
//	    return nil
//	}
type Validator interface {
	Validate() error
}

// validate performs tag-based required validation and then invokes the
// Validator interface if the config struct implements it. The cfg
// parameter is the original interface value (for Validator type
// assertion); rv is the dereferenced reflect.Value of the struct.
func validate(cfg any, rv reflect.Value) error {
	if err := validateRequired(rv, ""); err != nil {
		return err
	}

	if v, ok := cfg.(Validator); ok {
		if err := v.Validate(); err != nil {
			// Pass through sserr.Error instances unchanged.
			if _, isSSErr := sserr.AsError(err); isSSErr {
				return err
			}
			return sserr.Wrap(err, sserr.CodeValidation,
				"config: custom validation failed")
		}
	}

	return nil
}

// validateRequired recursively checks that all fields tagged with
// `required:"true"` hold non-zero values. The path parameter tracks
// the dotted field path for error messages (e.g., "Database.Host").
//
// Nested structs are traversed recursively. Unexported fields and
// non-struct types without a required tag are skipped.
func validateRequired(rv reflect.Value, path string) error {
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rv.Field(i)
		sf := rt.Field(i)

		if !field.CanSet() {
			continue
		}

		fieldPath := sf.Name
		if path != "" {
			fieldPath = path + "." + sf.Name
		}

		// Recurse into nested structs (but not named types like
		// time.Time that happen to be structs).
		if field.Kind() == reflect.Struct {
			if err := validateRequired(field, fieldPath); err != nil {
				return err
			}
			continue
		}

		if sf.Tag.Get("required") != "true" {
			continue
		}

		if field.IsZero() {
			return sserr.Newf(sserr.CodeValidationRequired,
				"config: required field %q is empty", fieldPath)
		}
	}

	return nil
}
