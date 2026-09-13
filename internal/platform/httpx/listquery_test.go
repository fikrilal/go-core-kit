package httpx_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

func testListConfig() httpx.ListQueryConfig {
	return httpx.ListQueryConfig{
		Sort: httpx.SortConfig{
			Allowed: []string{"createdAt", "email"},
			Default: []httpx.SortField{{Field: "createdAt", Desc: true}},
		},
		Filters: map[string][]httpx.FilterOp{
			"status":    {httpx.FilterOpEq, httpx.FilterOpIn},
			"createdAt": {httpx.FilterOpGte, httpx.FilterOpLte},
		},
		Search: true,
	}
}

func listRequest(t *testing.T, rawQuery string) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/things?"+rawQuery, nil)
}

func listIssues(t *testing.T, err error) []httpx.FieldIssue {
	t.Helper()
	var pe *httpx.ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want ProblemError", err)
	}
	if pe.Status != http.StatusBadRequest || pe.Code != httpx.CodeValidationFailed {
		t.Errorf("problem = %+v, want 400 VALIDATION_FAILED", pe)
	}
	return pe.Issues
}

func issueFields(issues []httpx.FieldIssue) []string {
	fields := make([]string, 0, len(issues))
	for _, issue := range issues {
		fields = append(fields, issue.Field)
	}
	return fields
}

func TestListQueryDefaults(t *testing.T) {
	query, err := httpx.ParseListQuery(listRequest(t, ""), testListConfig())
	if err != nil {
		t.Fatalf("ParseListQuery() error = %v", err)
	}
	if query.Limit != httpx.DefaultListLimit {
		t.Errorf("Limit = %d, want %d", query.Limit, httpx.DefaultListLimit)
	}
	if query.Cursor != "" || query.Search != "" || len(query.Filters) != 0 {
		t.Errorf("unexpected defaults: %+v", query)
	}
	if !slices.Equal(query.Sort, []httpx.SortField{{Field: "createdAt", Desc: true}}) {
		t.Errorf("Sort = %+v, want default createdAt desc", query.Sort)
	}
}

func TestListQueryParsesAll(t *testing.T) {
	raw := "limit=50&cursor=abc&sort=-email,createdAt&filter[status][in]=A,B&filter[createdAt][gte]=2026-01-01&q=hello"
	query, err := httpx.ParseListQuery(listRequest(t, raw), testListConfig())
	if err != nil {
		t.Fatalf("ParseListQuery() error = %v", err)
	}
	if query.Limit != 50 || query.Cursor != "abc" || query.Search != "hello" {
		t.Errorf("unexpected query: %+v", query)
	}
	wantSort := []httpx.SortField{{Field: "email", Desc: true}, {Field: "createdAt", Desc: false}}
	if !slices.Equal(query.Sort, wantSort) {
		t.Errorf("Sort = %+v, want %+v", query.Sort, wantSort)
	}
	if got := query.Filters["status"][httpx.FilterOpIn]; !slices.Equal(got, []string{"A", "B"}) {
		t.Errorf("status in = %v, want [A B]", got)
	}
	if got := query.Filters["createdAt"][httpx.FilterOpGte]; !slices.Equal(got, []string{"2026-01-01"}) {
		t.Errorf("createdAt gte = %v, want [2026-01-01]", got)
	}
}

func TestListQueryFilterEq(t *testing.T) {
	query, err := httpx.ParseListQuery(listRequest(t, "filter[status]=ACTIVE"), testListConfig())
	if err != nil {
		t.Fatalf("ParseListQuery() error = %v", err)
	}
	if got := query.Filters["status"][httpx.FilterOpEq]; !slices.Equal(got, []string{"ACTIVE"}) {
		t.Errorf("status eq = %v, want [ACTIVE]", got)
	}
}

func TestListQueryErrorsAccumulate(t *testing.T) {
	_, err := httpx.ParseListQuery(listRequest(t, "limit=abc&sort=bogus&filter[nope]=1"), testListConfig())
	fields := issueFields(listIssues(t, err))
	if !slices.Equal(fields, []string{"limit", "sort", "filter[nope]"}) {
		t.Errorf("issue fields = %v, want [limit sort filter[nope]]", fields)
	}
}

func TestListQueryLimitBounds(t *testing.T) {
	for _, raw := range []string{"limit=0", "limit=500", "limit=abc"} {
		_, err := httpx.ParseListQuery(listRequest(t, raw), testListConfig())
		if fields := issueFields(listIssues(t, err)); !slices.Equal(fields, []string{"limit"}) {
			t.Errorf("%s: issue fields = %v, want [limit]", raw, fields)
		}
	}

	cfg := testListConfig()
	cfg.MaxLimit = 50
	if _, err := httpx.ParseListQuery(listRequest(t, "limit=51"), cfg); err == nil {
		t.Error("limit=51 with MaxLimit=50 should fail")
	}
	if query, err := httpx.ParseListQuery(listRequest(t, "limit=50"), cfg); err != nil || query.Limit != 50 {
		t.Errorf("limit=50 with MaxLimit=50: query=%+v err=%v", query, err)
	}
}

func TestListQuerySortTooMany(t *testing.T) {
	_, err := httpx.ParseListQuery(listRequest(t, "sort=a,b,c,d"), testListConfig())
	if fields := issueFields(listIssues(t, err)); !slices.Equal(fields, []string{"sort"}) {
		t.Errorf("issue fields = %v, want [sort]", fields)
	}
}

func TestListQueryFilterOpNotAllowed(t *testing.T) {
	_, err := httpx.ParseListQuery(listRequest(t, "filter[status][gte]=x"), testListConfig())
	if fields := issueFields(listIssues(t, err)); !slices.Equal(fields, []string{"filter[status][gte]"}) {
		t.Errorf("issue fields = %v, want [filter[status][gte]]", fields)
	}
}

func TestListQueryFilterRepeated(t *testing.T) {
	_, err := httpx.ParseListQuery(listRequest(t, "filter[status]=A&filter[status]=B"), testListConfig())
	if fields := issueFields(listIssues(t, err)); !slices.Equal(fields, []string{"filter[status]"}) {
		t.Errorf("issue fields = %v, want [filter[status]]", fields)
	}
}

func TestListQueryMalformedFilterKey(t *testing.T) {
	_, err := httpx.ParseListQuery(listRequest(t, "filter[status]x=1"), testListConfig())
	if fields := issueFields(listIssues(t, err)); !slices.Equal(fields, []string{"filter[status]x"}) {
		t.Errorf("issue fields = %v, want [filter[status]x]", fields)
	}
}

func TestListQuerySearchDisabled(t *testing.T) {
	cfg := testListConfig()
	cfg.Search = false
	_, err := httpx.ParseListQuery(listRequest(t, "q=hello"), cfg)
	if fields := issueFields(listIssues(t, err)); !slices.Equal(fields, []string{"q"}) {
		t.Errorf("issue fields = %v, want [q]", fields)
	}
}

func TestListQueryErrorShapeThroughHandler(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc(testOp("GET", "/v1/things"), func(w http.ResponseWriter, r *http.Request) {
		query, err := httpx.ParseListQuery(r, testListConfig())
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.Data(w, r, query.Limit)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, listRequest(t, "limit=abc"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body problemResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "VALIDATION_FAILED" || len(body.Errors) != 1 || body.Errors[0].Field != "limit" {
		t.Errorf("unexpected problem: %+v", body)
	}
}

func TestListQuerySuccessThroughHandler(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc(testOp("GET", "/v1/things"), func(w http.ResponseWriter, r *http.Request) {
		query, err := httpx.ParseListQuery(r, testListConfig())
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.List(w, r, []item{}, httpx.ListMeta{Limit: query.Limit})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, listRequest(t, "limit=10"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Meta httpx.ListMeta `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Meta.Limit != 10 {
		t.Errorf("meta.limit = %d, want 10", body.Meta.Limit)
	}
}
