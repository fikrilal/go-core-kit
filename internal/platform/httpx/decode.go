package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

var validate = newValidator()

// Decode parses the JSON request body into v and validates it against validate tags.
func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return invalidBody()
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return invalidBody()
	}
	return validateStruct(v)
}

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
	return v
}

func validateStruct(v any) error {
	err := validate.Struct(v)
	if err == nil {
		return nil
	}
	var fieldErrors validator.ValidationErrors
	if !errors.As(err, &fieldErrors) {
		return fmt.Errorf("validate: %w", err)
	}
	issues := make([]FieldIssue, 0, len(fieldErrors))
	for _, fe := range fieldErrors {
		issues = append(issues, FieldIssue{Field: fe.Field(), Message: validationMessage(fe)})
	}
	return &ProblemError{Status: http.StatusBadRequest, Code: CodeValidationFailed, Issues: issues}
}

func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email"
	case "uuid":
		return "must be a valid UUID"
	case "oneof":
		return "must be one of: " + strings.ReplaceAll(fe.Param(), " ", ", ")
	case "min", "gte":
		return "must be at least " + fe.Param()
	case "max", "lte":
		return "must be at most " + fe.Param()
	case "len":
		return "must have length " + fe.Param()
	}
	return "is invalid"
}

func invalidBody() *ProblemError {
	return &ProblemError{Status: http.StatusBadRequest, Code: CodeValidationFailed, Detail: "invalid JSON body"}
}
