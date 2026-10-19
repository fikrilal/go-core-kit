package main

import (
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// updateGolden regenerates the captured response shapes instead of comparing them.
var updateGolden = flag.Bool("update-golden", false, "rewrite the golden response shapes")

// looksLikeIdentifier reports whether a path segment is a generated id rather than a
// literal route part: a UUID, or anything long enough that it cannot be a word.
func looksLikeIdentifier(segment string) bool {
	if len(segment) >= 24 {
		return true
	}
	return uuidPattern.MatchString(segment)
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// goldenShape is what a golden file records: the status, the envelope keys the
// response carried (sorted, so key order is not contract), and the problem code for
// a refusal. Values are deliberately absent: this is the wire *shape* oracle, not a
// content fixture, so ordinary data changes do not churn it.
type goldenShape struct {
	Status int      `json:"status"`
	Keys   []string `json:"keys"`
	Code   string   `json:"code,omitempty"`
}

// goldenName turns a response into the capture's file name. The path alone is not
// enough: one route answers a success and several refusals, and each shape deserves
// its own capture. A changed status or code therefore lands on a name whose file does
// not exist, which is exactly the failure this oracle is for.
func goldenName(req *http.Request, shape goldenShape) string {
	trimmed := strings.Trim(strings.TrimPrefix(req.URL.Path, "/"), "/")
	segments := strings.Split(trimmed, "/")
	for i, segment := range segments {
		if looksLikeIdentifier(segment) {
			segments[i] = "{id}"
		}
	}
	name := strings.ReplaceAll(strings.Join(segments, "."), "{id}", "_id_") + "." + strconv.Itoa(shape.Status)
	if shape.Code != "" {
		name += "." + strings.ToLower(shape.Code)
	}
	return name + ".json"
}

// assertGoldenShapePath compares one response against the capture for its route's
// path, and writes the capture when -update-golden is passed. The golden file is
// keyed by path rather than operation id, which is what a validator at the call site
// can see.
func assertGoldenShapePath(t *testing.T, req *http.Request, status int, body []byte) {
	t.Helper()
	operation := strings.TrimSuffix(goldenName(req, goldenShape{Status: status, Code: problemCode(t, body)}), ".json")
	got := goldenShape{Status: status, Keys: envelopeKeys(t, body), Code: problemCode(t, body)}
	path := filepath.Join("testdata", "golden", goldenName(req, got))

	if *updateGolden {
		encoded, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatalf("encode golden %s: %v", operation, err)
		}
		if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
			t.Fatalf("write golden %s: %v", operation, err)
		}
		return
	}

	raw, err := os.ReadFile(path) //nolint:gosec // the path is the test's own golden file
	if err != nil {
		t.Fatalf("read golden %s: %v (run go test ./cmd/api -update-golden to capture it)", operation, err)
	}
	var want goldenShape
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode golden %s: %v", operation, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s shape changed:\n got %+v\nwant %+v\n(re-run with -update-golden if the contract changed)", operation, got, want)
	}
}

// envelopeKeys lists the top-level keys of a success envelope, empty for a body
// that carries nothing.
func envelopeKeys(t *testing.T, body []byte) []string {
	t.Helper()
	if len(body) == 0 {
		return nil
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil
	}
	keys := make([]string, 0, len(decoded))
	for key := range decoded {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// problemCode reads the error code from a problem response.
func problemCode(t *testing.T, body []byte) string {
	t.Helper()
	if len(body) == 0 {
		return ""
	}
	var decoded struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ""
	}
	return decoded.Code
}
