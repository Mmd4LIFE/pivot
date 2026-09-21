package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/Mmd4LIFE/pivot/internal/logging"
	"github.com/Mmd4LIFE/pivot/internal/store/repo"
	"github.com/Mmd4LIFE/pivot/internal/tenant"
)

// Code is a stable, machine-readable error identifier.
//
// Codes are deliberately independent of HTTP status. A status says how the
// transport should behave; a code says what went wrong, and clients branch on
// it. Two different failures that both return 409 need to be distinguishable
// without parsing prose.
//
// Once published, a code's meaning never changes and it is never reused. New
// failures get new codes. That is what lets a client written today still
// understand an error next year, and what makes the documentation URL in every
// response a durable link.
type Code string

// The registry. Format: PIVOT-<AREA>-<NNN>.
const (
	// Authentication and tenancy.
	CodeUnauthorized   Code = "PIVOT-AUTH-001"
	CodeForbidden      Code = "PIVOT-AUTH-002"
	CodeTenantUnknown  Code = "PIVOT-AUTH-003"
	CodeSessionExpired Code = "PIVOT-AUTH-004"

	// CodeAccountLocked is a 429, like CodeRateLimited, and the pair is the
	// reason codes exist independently of status: a client that can only see
	// "429" cannot tell "slow down" from "this account is locked", and the two
	// call for completely different handling in the UI.
	CodeAccountLocked Code = "PIVOT-AUTH-005"

	// Request shape.
	CodeMalformedJSON    Code = "PIVOT-REQ-001"
	CodeValidationFailed Code = "PIVOT-REQ-002"
	CodeNotFound         Code = "PIVOT-REQ-003"
	CodeMethodNotAllowed Code = "PIVOT-REQ-004"
	CodeUnsupportedMedia Code = "PIVOT-REQ-005"
	CodePayloadTooLarge  Code = "PIVOT-REQ-006"

	// Data.
	CodeVersionConflict Code = "PIVOT-DATA-001"
	CodeDuplicate       Code = "PIVOT-DATA-002"

	// Throttling.
	CodeRateLimited Code = "PIVOT-RATE-001"

	// Server.
	CodeInternal    Code = "PIVOT-SRV-001"
	CodeUnavailable Code = "PIVOT-SRV-002"
)

// codeInfo is what a code means, independent of any single occurrence.
type codeInfo struct {
	status  int
	summary string
}

// codes is the authoritative registry. A code that is not here has no defined
// status, which a test treats as a bug rather than defaulting silently to 500.
var codes = map[Code]codeInfo{
	CodeUnauthorized:   {http.StatusUnauthorized, "Authentication is required or has failed"},
	CodeForbidden:      {http.StatusForbidden, "The caller is authenticated but not permitted"},
	CodeTenantUnknown:  {http.StatusUnauthorized, "The request could not be attributed to an organization"},
	CodeSessionExpired: {http.StatusUnauthorized, "The session is no longer valid"},
	CodeAccountLocked:  {http.StatusTooManyRequests, "Too many failed attempts; the account is temporarily locked"},

	CodeMalformedJSON:    {http.StatusBadRequest, "The request body is not valid JSON"},
	CodeValidationFailed: {http.StatusUnprocessableEntity, "The request body failed validation"},
	CodeNotFound:         {http.StatusNotFound, "No such resource"},
	CodeMethodNotAllowed: {http.StatusMethodNotAllowed, "That method is not supported on this path"},
	CodeUnsupportedMedia: {http.StatusUnsupportedMediaType, "The content type is not supported"},
	CodePayloadTooLarge:  {http.StatusRequestEntityTooLarge, "The request body is too large"},

	CodeVersionConflict: {http.StatusConflict, "The resource was modified since it was read"},
	CodeDuplicate:       {http.StatusConflict, "A resource with those values already exists"},

	CodeRateLimited: {http.StatusTooManyRequests, "Too many requests"},

	CodeInternal:    {http.StatusInternalServerError, "An unexpected error occurred"},
	CodeUnavailable: {http.StatusServiceUnavailable, "The service is temporarily unavailable"},
}

// DocsBaseURL is where an error code's explanation lives. Every response
// carries the link, so an operator never has to search for the meaning of a
// code they have never seen.
const DocsBaseURL = "https://docs.pivot.dev/errors/"

// Status returns the HTTP status for a code.
func (c Code) Status() int {
	if info, ok := codes[c]; ok {
		return info.status
	}

	return http.StatusInternalServerError
}

// Summary returns the code's stable description.
func (c Code) Summary() string {
	if info, ok := codes[c]; ok {
		return info.summary
	}

	return "Unknown error"
}

// DocsURL returns the documentation link for a code.
func (c Code) DocsURL() string { return DocsBaseURL + string(c) }

// Known reports whether a code is registered.
func (c Code) Known() bool {
	_, ok := codes[c]

	return ok
}

// RegisteredCodes returns every code, for tests and documentation generation.
func RegisteredCodes() []Code {
	out := make([]Code, 0, len(codes))
	for c := range codes {
		out = append(out, c)
	}

	return out
}

// Detail is one specific problem within an error, typically a bad field.
type Detail struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// ErrorBody is the error envelope. Every non-2xx response has this shape —
// including 404s and panics — so a client needs exactly one error path.
type ErrorBody struct {
	Code      Code     `json:"code"`
	Message   string   `json:"message"`
	Details   []Detail `json:"details,omitempty"`
	RequestID string   `json:"requestId,omitempty"`
	Docs      string   `json:"docs"`
}

// ErrorResponse wraps the envelope, so a success body and an error body can
// never be confused by a client inspecting the top-level keys.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// APIError is an error that carries a code and optional details.
type APIError struct {
	Code    Code
	Message string
	Details []Detail

	// Err is the underlying cause. It is logged, never sent: internal errors
	// leak implementation detail and sometimes credentials.
	Err error
}

func (e *APIError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}

	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap supports errors.Is and errors.As on the cause.
func (e *APIError) Unwrap() error { return e.Err }

// Errorf builds an APIError with a formatted message.
func Errorf(code Code, format string, args ...any) *APIError {
	return &APIError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// NewError builds an APIError wrapping a cause.
func NewError(code Code, message string, cause error) *APIError {
	return &APIError{Code: code, Message: message, Err: cause}
}

// ValidationError builds a 422 carrying per-field details.
func ValidationError(details ...Detail) *APIError {
	return &APIError{
		Code:    CodeValidationFailed,
		Message: "The request body failed validation",
		Details: details,
	}
}

// FromError maps any error onto an APIError.
//
// This is the single translation point between the layers below and the HTTP
// surface. Repository and tenancy errors get meaningful codes; anything
// unrecognized becomes an internal error with its detail withheld, because an
// unexpected error is exactly the kind most likely to contain something a
// caller should not see.
func FromError(err error) *APIError {
	if err == nil {
		return nil
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}

	switch {
	case errors.Is(err, repo.ErrNotFound):
		return &APIError{Code: CodeNotFound, Message: CodeNotFound.Summary(), Err: err}

	case errors.Is(err, repo.ErrConflict):
		return &APIError{Code: CodeVersionConflict, Message: CodeVersionConflict.Summary(), Err: err}

	case errors.Is(err, repo.ErrDuplicate):
		return &APIError{Code: CodeDuplicate, Message: CodeDuplicate.Summary(), Err: err}

	case errors.Is(err, tenant.ErrNoScope), errors.Is(err, tenant.ErrInvalidScope):
		return &APIError{Code: CodeTenantUnknown, Message: CodeTenantUnknown.Summary(), Err: err}

	default:
		return &APIError{Code: CodeInternal, Message: CodeInternal.Summary(), Err: err}
	}
}

// WriteError sends an error response.
//
// A 5xx is logged at error level with its cause; a 4xx is logged at debug,
// because client mistakes are normal traffic and logging them at warn turns
// a scanner into an alert storm.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := FromError(err)
	ctx := r.Context()
	requestID := RequestIDFrom(ctx)

	status := apiErr.Code.Status()
	log := logging.FromContext(ctx)

	attrs := []any{
		slog.String("code", string(apiErr.Code)),
		slog.Int("status", status),
		slog.String("path", r.URL.Path),
	}

	if apiErr.Err != nil {
		attrs = append(attrs, logging.Err(apiErr.Err))
	}

	if status >= http.StatusInternalServerError {
		log.Error("request failed", attrs...)
	} else {
		log.Debug("request rejected", attrs...)
	}

	WriteJSON(ctx, w, status, ErrorResponse{Error: ErrorBody{
		Code:      apiErr.Code,
		Message:   apiErr.Message,
		Details:   apiErr.Details,
		RequestID: requestID,
		Docs:      apiErr.Code.DocsURL(),
	}})
}

// WriteJSON writes a JSON response with the standard headers.
func WriteJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	if body == nil {
		return
	}

	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so this can only be logged.
		logging.FromContext(ctx).Error("write response", logging.Err(err))
	}
}
