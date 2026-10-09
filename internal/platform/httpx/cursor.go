package httpx

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// cursorVersion tags the opaque cursor payload.
const cursorVersion = 1

// Cursor is the decoded form of an opaque list cursor: the normalized primary
// sort it was built for and the last row's sort values.
type Cursor struct {
	Sort  string
	After map[string]string
}

type cursorPayload struct {
	V     int               `json:"v"`
	Sort  string            `json:"sort"`
	After map[string]string `json:"after"`
}

// EncodeCursor builds the opaque cursor for the last row of a page. The
// payload is strings only, so marshaling cannot fail.
func EncodeCursor(sort string, after map[string]string) string {
	payload, _ := json.Marshal(cursorPayload{V: cursorVersion, Sort: sort, After: after})
	return base64.RawURLEncoding.EncodeToString(payload)
}

// DecodeCursor parses an opaque cursor and checks it was built for the given
// normalized sort with an entry for every field. The returned error is a 400
// VALIDATION_FAILED on "cursor".
func DecodeCursor(raw, sort string, fields []string) (Cursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Cursor{}, invalidCursor()
	}
	var payload cursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return Cursor{}, invalidCursor()
	}
	if payload.V != cursorVersion || payload.Sort == "" || len(payload.After) == 0 {
		return Cursor{}, invalidCursor()
	}
	if payload.Sort != sort {
		return Cursor{}, invalidCursor()
	}
	for _, field := range fields {
		if _, ok := payload.After[field]; !ok {
			return Cursor{}, invalidCursor()
		}
	}
	return Cursor{Sort: payload.Sort, After: payload.After}, nil
}

func invalidCursor() *ProblemError {
	return &ProblemError{
		Status: http.StatusBadRequest,
		Code:   CodeValidationFailed,
		Issues: []FieldIssue{{Field: "cursor", Message: "is invalid"}},
	}
}
