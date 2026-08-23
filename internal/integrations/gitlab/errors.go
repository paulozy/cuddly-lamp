package gitlab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// maxErrorBodyBytes caps how much of a failure response is read, matching the
// limit the request path already applied inline.
const maxErrorBodyBytes = 4096

// APIError is a rejection the API answered with a status this package has no
// sentinel for.
//
// The type is duplicated from the GitHub client on purpose: both client
// packages speak their own wire format and know nothing about each other or
// about the neutral `scm` types. Sharing it would be the first shared thing,
// and the translation layer is where commonality belongs.
type APIError struct {
	// Status is the upstream HTTP status, preserved so callers can decide
	// whether the rejection is permanent.
	Status int
	// Message is whatever GitLab put in `message` or `error`. It sends either
	// depending on the endpoint.
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gitlab API error %d: %s", e.Status, e.Message)
}

// errorPayload covers both envelopes GitLab uses. `message` is also sometimes
// an object keyed by field rather than a string, so it stays raw.
type errorPayload struct {
	Message json.RawMessage `json:"message"`
	Error   string          `json:"error"`
}

// newAPIError builds an APIError from a failure body. An unparseable body
// still yields a usable error: the status alone classifies the rejection.
func newAPIError(status int, raw []byte) *APIError {
	apiErr := &APIError{Status: status, Message: strings.TrimSpace(string(raw))}

	var payload errorPayload
	if err := json.Unmarshal(raw, &payload); err == nil {
		if message := flattenMessage(payload.Message); message != "" {
			apiErr.Message = message
		} else if payload.Error != "" {
			apiErr.Message = payload.Error
		}
	}

	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(status)
	}
	return apiErr
}

// flattenMessage reads GitLab's `message`, which is a string on some endpoints
// and a map of field name to problems on others (validation failures).
func flattenMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}

	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		return strings.TrimSpace(strings.Join(asStrings, "; "))
	}

	var asMap map[string][]string
	if err := json.Unmarshal(raw, &asMap); err == nil {
		parts := make([]string, 0, len(asMap))
		for field, problems := range asMap {
			parts = append(parts, field+" "+strings.Join(problems, ", "))
		}
		// Sorted so the same failure does not produce a different string on
		// every request — Go randomizes map iteration order.
		sortStrings(parts)
		return strings.TrimSpace(strings.Join(parts, "; "))
	}

	return ""
}

// sortStrings is a tiny insertion sort, used instead of importing sort for one
// call on a slice that is never more than a handful of entries.
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
