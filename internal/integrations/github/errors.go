package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// maxErrorBodyBytes caps how much of a failure response is read. GitHub does
// not bound the size of an error payload, and the whole thing may end up in a
// log line or an API response, so it is truncated here rather than downstream.
// The value matches the GitLab client's existing limit.
const maxErrorBodyBytes = 4096

// APIError is a rejection the API answered with a status this package has no
// sentinel for.
//
// It exists because the status is the only thing that tells a caller whether
// retrying could ever help. Formatting the failure into a string — which is
// what this code used to do — threw that away, so a 422 "you cannot approve
// your own pull request" became indistinguishable from a transient outage and
// was reported as one.
type APIError struct {
	// Status is the upstream HTTP status, preserved so callers can decide
	// whether the rejection is permanent.
	Status int
	// Message is GitHub's top-level `message` field.
	Message string
	// Errors holds the per-problem detail from the `errors` array, which is
	// where the actionable sentence usually lives — "Review Can not approve
	// your own pull request" arrives here, not in Message.
	Errors []string
}

func (e *APIError) Error() string {
	if detail := strings.Join(e.Errors, "; "); detail != "" {
		return fmt.Sprintf("github API error %d: %s (%s)", e.Status, e.Message, detail)
	}
	return fmt.Sprintf("github API error %d: %s", e.Status, e.Message)
}

// errorPayload mirrors GitHub's error envelope. `errors` is deliberately raw:
// GitHub sends it both as a list of strings and as a list of objects depending
// on the endpoint, and a mismatch there must not cost us the status.
type errorPayload struct {
	Message string          `json:"message"`
	Errors  json.RawMessage `json:"errors"`
}

// errorFieldObject is the object form of an `errors` entry. Only the human-
// readable parts are kept; `resource` and `field` are noise for a message that
// ends up in front of a person.
type errorFieldObject struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

// newAPIError builds an APIError from a failure body. A body that does not
// parse is not an error in itself — the status still classifies the failure —
// so the raw text becomes the message and the caller keeps working.
func newAPIError(status int, raw []byte) *APIError {
	apiErr := &APIError{Status: status}

	var payload errorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		apiErr.Message = strings.TrimSpace(string(raw))
		if apiErr.Message == "" {
			apiErr.Message = http.StatusText(status)
		}
		return apiErr
	}

	apiErr.Message = payload.Message
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(status)
	}
	apiErr.Errors = parseErrorDetails(payload.Errors)
	return apiErr
}

// parseErrorDetails handles both shapes of GitHub's `errors` array. Anything it
// cannot read yields no details rather than a failure: the status and the
// top-level message already carry enough to classify the rejection.
func parseErrorDetails(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		return trimNonEmpty(asStrings)
	}

	var asObjects []errorFieldObject
	if err := json.Unmarshal(raw, &asObjects); err == nil {
		out := make([]string, 0, len(asObjects))
		for _, item := range asObjects {
			if item.Message != "" {
				out = append(out, item.Message)
				continue
			}
			// A `code` with no message is still a reason — "already_exists"
			// says more than dropping the entry entirely.
			if item.Code != "" {
				out = append(out, item.Code)
			}
		}
		return trimNonEmpty(out)
	}

	return nil
}

func trimNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isRateLimited reports whether a 403 is throttling rather than a permission
// problem.
//
// GitHub uses 403 for both, which is why this check exists: treating every 403
// as a rate limit told people to wait when the real answer was that their
// token could not write. The signals are the documented ones — an exhausted
// quota, or an explicit backoff instruction for the secondary limit.
func isRateLimited(header http.Header) bool {
	if header.Get("Retry-After") != "" {
		return true
	}
	return header.Get("X-RateLimit-Remaining") == "0"
}
