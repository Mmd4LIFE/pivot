package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Validator is implemented by request bodies that can check themselves.
//
// Validation lives on the type rather than in the handler so it cannot be
// skipped by a handler that forgets, and so the rules sit next to the fields
// they describe.
type Validator interface {
	// Validate returns one Detail per problem. Returning all of them at once
	// means a client fixing a form sees every error, not the first.
	Validate() []Detail
}

// DefaultMaxBodyBytes bounds a request body. Generous for JSON, small enough
// that a malicious body cannot be decoded into memory.
const DefaultMaxBodyBytes = 1 << 20 // 1 MiB

// Decode reads and validates a JSON request body.
//
// It rejects the wrong content type, malformed JSON, unknown fields, trailing
// content, and anything the body's own Validate rejects — all as coded errors,
// so a handler's first line is a decode and its second is business logic.
//
// Unknown fields are an error rather than being ignored: silently discarding
// `{"emial": "..."}` produces a user who swears they set a value that never
// arrived.
func Decode[T any](w http.ResponseWriter, r *http.Request, dst *T) error {
	if err := requireJSON(r); err != nil {
		return err
	}

	r.Body = http.MaxBytesReader(w, r.Body, DefaultMaxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}

	// A second value means the caller sent more than one JSON document, which
	// is never intentional and can hide a smuggled payload.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Errorf(CodeMalformedJSON, "The request body must contain exactly one JSON object")
	}

	if v, ok := any(dst).(Validator); ok {
		if details := v.Validate(); len(details) > 0 {
			return ValidationError(details...)
		}
	}

	return nil
}

// requireJSON rejects a body that is not declared as JSON.
func requireJSON(r *http.Request) error {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return Errorf(CodeUnsupportedMedia, "A Content-Type of application/json is required")
	}

	// Strip parameters such as "; charset=utf-8".
	mediaType, _, _ := strings.Cut(ct, ";")

	if !strings.EqualFold(strings.TrimSpace(mediaType), "application/json") {
		return Errorf(CodeUnsupportedMedia, "Content-Type %q is not supported; use application/json", mediaType)
	}

	return nil
}

// decodeError maps a JSON decoding failure onto a coded error.
//
// The messages name the offending field and position where the decoder knows
// them, because "invalid character" with no location is the kind of error that
// costs somebody an afternoon.
func decodeError(err error) error {
	var (
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
		maxErr    *http.MaxBytesError
	)

	switch {
	case errors.As(err, &maxErr):
		return Errorf(CodePayloadTooLarge,
			"The request body exceeds the %d byte limit", maxErr.Limit)

	case errors.As(err, &syntaxErr):
		return Errorf(CodeMalformedJSON,
			"The request body contains malformed JSON at byte %d", syntaxErr.Offset)

	case errors.As(err, &typeErr):
		if typeErr.Field != "" {
			return ValidationError(Detail{
				Field:   typeErr.Field,
				Message: fmt.Sprintf("expected %s", typeErr.Type.String()),
			})
		}

		return Errorf(CodeMalformedJSON,
			"The request body has the wrong type at byte %d", typeErr.Offset)

	case errors.Is(err, io.EOF):
		return Errorf(CodeMalformedJSON, "The request body is empty")

	case strings.HasPrefix(err.Error(), "json: unknown field "):
		field := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)

		return ValidationError(Detail{Field: field, Message: "unknown field"})

	default:
		return NewError(CodeMalformedJSON, "The request body could not be decoded", err)
	}
}

// --- validation helpers ---------------------------------------------------

// Required returns a Detail when a string value is empty.
func Required(field, value string) *Detail {
	if strings.TrimSpace(value) == "" {
		return &Detail{Field: field, Message: "is required"}
	}

	return nil
}

// MaxLen returns a Detail when a string exceeds a length.
func MaxLen(field, value string, maximum int) *Detail {
	if len(value) > maximum {
		return &Detail{Field: field, Message: fmt.Sprintf("must be at most %d characters", maximum)}
	}

	return nil
}

// OneOf returns a Detail when a value is outside an allowed set.
func OneOf(field, value string, allowed ...string) *Detail {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}

	return &Detail{Field: field, Message: "must be one of " + strings.Join(allowed, ", ")}
}

// Collect gathers non-nil details, so a Validate method reads as a list of
// rules rather than a stack of if statements.
func Collect(details ...*Detail) []Detail {
	out := make([]Detail, 0, len(details))

	for _, d := range details {
		if d != nil {
			out = append(out, *d)
		}
	}

	return out
}
