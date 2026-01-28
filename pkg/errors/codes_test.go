package errors

import (
	"testing"
)

func TestCode_String(t *testing.T) {
	tests := []struct {
		name string
		code Code
		want string
	}{
		{
			name: "validation code",
			code: CodeValidation,
			want: "VAL_001",
		},
		{
			name: "authentication code",
			code: CodeAuthentication,
			want: "AUTH_001",
		},
		{
			name: "authorization code",
			code: CodeAuthorization,
			want: "AUTHZ_001",
		},
		{
			name: "not found code",
			code: CodeNotFound,
			want: "NF_001",
		},
		{
			name: "conflict code",
			code: CodeConflict,
			want: "CONF_001",
		},
		{
			name: "internal code",
			code: CodeInternal,
			want: "INT_001",
		},
		{
			name: "unavailable code",
			code: CodeUnavailable,
			want: "UNAVAIL_001",
		},
		{
			name: "timeout code",
			code: CodeTimeout,
			want: "TIMEOUT_001",
		},
		{
			name: "empty code",
			code: Code(""),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.code.String(); got != tt.want {
				t.Errorf("Code.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCode_Category(t *testing.T) {
	tests := []struct {
		name string
		code Code
		want string
	}{
		{
			name: "validation category",
			code: CodeValidation,
			want: "VAL",
		},
		{
			name: "validation required category",
			code: CodeValidationRequired,
			want: "VAL",
		},
		{
			name: "authentication category",
			code: CodeAuthentication,
			want: "AUTH",
		},
		{
			name: "authentication expired category",
			code: CodeAuthenticationExpired,
			want: "AUTH",
		},
		{
			name: "authorization category",
			code: CodeAuthorization,
			want: "AUTHZ",
		},
		{
			name: "authorization denied category",
			code: CodeAuthorizationDenied,
			want: "AUTHZ",
		},
		{
			name: "not found category",
			code: CodeNotFound,
			want: "NF",
		},
		{
			name: "not found user category",
			code: CodeNotFoundUser,
			want: "NF",
		},
		{
			name: "conflict category",
			code: CodeConflict,
			want: "CONF",
		},
		{
			name: "conflict already exists category",
			code: CodeConflictAlreadyExists,
			want: "CONF",
		},
		{
			name: "internal category",
			code: CodeInternal,
			want: "INT",
		},
		{
			name: "internal database category",
			code: CodeInternalDatabase,
			want: "INT",
		},
		{
			name: "unavailable category",
			code: CodeUnavailable,
			want: "UNAVAIL",
		},
		{
			name: "unavailable dependency category",
			code: CodeUnavailableDependency,
			want: "UNAVAIL",
		},
		{
			name: "timeout category",
			code: CodeTimeout,
			want: "TIMEOUT",
		},
		{
			name: "timeout database category",
			code: CodeTimeoutDatabase,
			want: "TIMEOUT",
		},
		{
			name: "code without underscore returns entire string",
			code: Code("NOCATEGORY"),
			want: "NOCATEGORY",
		},
		{
			name: "empty code returns empty string",
			code: Code(""),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.code.Category(); got != tt.want {
				t.Errorf("Code.Category() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllCodesHaveValidFormat(t *testing.T) {
	// Verify all defined codes follow the CATEGORY_XXX format
	codes := []Code{
		CodeValidation, CodeValidationRequired, CodeValidationFormat, CodeValidationRange,
		CodeAuthentication, CodeAuthenticationExpired, CodeAuthenticationInvalid,
		CodeAuthorization, CodeAuthorizationDenied, CodeAuthorizationInsufficientScope,
		CodeNotFound, CodeNotFoundUser, CodeNotFoundResource,
		CodeConflict, CodeConflictAlreadyExists, CodeConflictVersionMismatch,
		CodeInternal, CodeInternalDatabase, CodeInternalConfiguration,
		CodeUnavailable, CodeUnavailableDependency, CodeUnavailableOverloaded,
		CodeTimeout, CodeTimeoutDatabase, CodeTimeoutDependency,
	}

	for _, code := range codes {
		t.Run(string(code), func(t *testing.T) {
			s := code.String()
			if s == "" {
				t.Error("Code.String() returned empty string")
			}

			cat := code.Category()
			if cat == "" {
				t.Error("Code.Category() returned empty string")
			}

			// Verify category is a known category
			validCategories := map[string]bool{
				"VAL": true, "AUTH": true, "AUTHZ": true, "NF": true,
				"CONF": true, "INT": true, "UNAVAIL": true, "TIMEOUT": true,
			}
			if !validCategories[cat] {
				t.Errorf("Code.Category() = %v, not a valid category", cat)
			}
		})
	}
}
