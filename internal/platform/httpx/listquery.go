package httpx

import (
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// List query defaults and bounds.
const (
	DefaultListLimit = 25
	MaxListLimit     = 250
	MaxSortFields    = 3
)

// filter[field] and filter[field][op]. The op group is lowercase only, so
// filter[status][GTE] is "not a valid filter", not "operator not allowed".
var filterKeyPattern = regexp.MustCompile(`^filter\[([A-Za-z0-9_]+)\](?:\[([a-z]+)\])?$`)

// ListQuery is the parsed list request: pagination, sort, filters, and search.
type ListQuery struct {
	Limit   int
	Cursor  string
	Sort    []SortField
	Filters map[string]map[FilterOp][]string
	Search  string
}

// SortField is one ordered sort key.
type SortField struct {
	Field string
	Desc  bool
}

// FilterOp is a supported filter operator.
type FilterOp string

// Filter operators.
const (
	FilterOpEq  FilterOp = "eq"
	FilterOpIn  FilterOp = "in"
	FilterOpGte FilterOp = "gte"
	FilterOpLte FilterOp = "lte"
)

// ListQueryConfig declares what an endpoint allows.
type ListQueryConfig struct {
	DefaultLimit int
	MaxLimit     int
	Sort         SortConfig
	Filters      map[string][]FilterOp
	Search       bool
}

// SortConfig declares the allowed and default sort fields.
type SortConfig struct {
	Allowed []string
	Default []SortField
}

// ParseListQuery parses and validates list query parameters against the endpoint config.
func ParseListQuery(r *http.Request, cfg ListQueryConfig) (ListQuery, error) {
	values := r.URL.Query()
	p := &listQueryParser{
		cfg:    cfg,
		values: values,
		query: ListQuery{
			Limit:   cfg.limitDefault(),
			Cursor:  values.Get("cursor"),
			Sort:    slices.Clone(cfg.Sort.Default),
			Filters: map[string]map[FilterOp][]string{},
		},
	}
	p.parseLimit()
	p.parseSort()
	p.parseFilters()
	p.parseSearch()
	if len(p.issues) > 0 {
		return ListQuery{}, &ProblemError{
			Status: http.StatusBadRequest,
			Code:   CodeValidationFailed,
			Issues: p.issues,
		}
	}
	return p.query, nil
}

func (c ListQueryConfig) limitDefault() int {
	if c.DefaultLimit > 0 {
		return c.DefaultLimit
	}
	return DefaultListLimit
}

func (c ListQueryConfig) maxLimit() int {
	if c.MaxLimit > 0 {
		return c.MaxLimit
	}
	return MaxListLimit
}

type listQueryParser struct {
	cfg    ListQueryConfig
	values url.Values
	query  ListQuery
	issues []FieldIssue
}

func (p *listQueryParser) addIssue(field, message string) {
	p.issues = append(p.issues, FieldIssue{Field: field, Message: message})
}

func (p *listQueryParser) parseLimit() {
	raw := p.values.Get("limit")
	if raw == "" {
		return
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		p.addIssue("limit", "must be an integer")
		return
	}
	maxLimit := p.cfg.maxLimit()
	if limit < 1 || limit > maxLimit {
		p.addIssue("limit", fmt.Sprintf("must be between 1 and %d", maxLimit))
		return
	}
	p.query.Limit = limit
}

func (p *listQueryParser) parseSort() {
	raw := p.values.Get("sort")
	if raw == "" {
		return
	}
	fields := strings.Split(raw, ",")
	if len(fields) > MaxSortFields {
		p.addIssue("sort", fmt.Sprintf("must not exceed %d fields", MaxSortFields))
		return
	}
	parsed := make([]SortField, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		desc := strings.HasPrefix(field, "-")
		field = strings.TrimPrefix(field, "-")
		if !slices.Contains(p.cfg.Sort.Allowed, field) {
			p.addIssue("sort", fmt.Sprintf("unknown field %q", field))
			continue
		}
		parsed = append(parsed, SortField{Field: field, Desc: desc})
	}
	p.query.Sort = parsed
}

func (p *listQueryParser) parseFilters() {
	for _, key := range slices.Sorted(maps.Keys(p.values)) {
		if strings.HasPrefix(key, "filter[") {
			p.parseFilter(key, p.values[key])
		}
	}
}

func (p *listQueryParser) parseFilter(key string, values []string) {
	match := filterKeyPattern.FindStringSubmatch(key)
	if match == nil {
		p.addIssue(key, "is not a valid filter")
		return
	}
	field := match[1]
	op := FilterOp(match[2])
	if op == "" {
		op = FilterOpEq
	}
	allowedOps, ok := p.cfg.Filters[field]
	if !ok {
		p.addIssue(key, "is not a filterable field")
		return
	}
	if !slices.Contains(allowedOps, op) {
		p.addIssue(key, fmt.Sprintf("does not support the %q operator", op))
		return
	}
	if len(values) != 1 {
		p.addIssue(key, "must not be repeated")
		return
	}
	ops, ok := p.query.Filters[field]
	if !ok {
		ops = map[FilterOp][]string{}
		p.query.Filters[field] = ops
	}
	ops[op] = filterValues(op, values[0])
}

func (p *listQueryParser) parseSearch() {
	raw := p.values.Get("q")
	if raw == "" {
		return
	}
	if !p.cfg.Search {
		p.addIssue("q", "is not supported")
		return
	}
	p.query.Search = raw
}

func filterValues(op FilterOp, raw string) []string {
	if op != FilterOpIn {
		return []string{raw}
	}
	values := strings.Split(raw, ",")
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}
	return values
}
