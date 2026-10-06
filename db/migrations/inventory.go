package migrations

import (
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
)

// inventory maps a table name to its column names.
type inventory map[string]map[string]struct{}

var (
	createTablePattern = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	alterColumnPattern = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+([A-Za-z_][A-Za-z0-9_]*)\s+ADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
)

// readUpSQL concatenates the goose Up sections of every .sql file in fsys.
func readUpSQL(fsys fs.FS) (string, error) {
	var files []string
	err := fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".sql") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("read migration tree: %w", err)
	}
	slices.Sort(files)
	var b strings.Builder
	for _, path := range files {
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		b.WriteString(upSection(string(raw)))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// parseUpSQL reads table and column names from a goose Up script.
// Comments are removed first, and only outside quoted strings.
func parseUpSQL(sqlText string) inventory {
	sqlText = stripComments(upSection(sqlText))
	parsed := inventory{}
	for _, match := range createTablePattern.FindAllStringSubmatchIndex(sqlText, -1) {
		name := sqlText[match[2]:match[3]]
		open := match[1] - 1
		body, _ := matchingParen(sqlText, open)
		addColumns(parsed, name, columnNames(body))
	}
	for _, match := range alterColumnPattern.FindAllStringSubmatch(sqlText, -1) {
		column := match[2]
		if isTableConstraint(column) {
			continue
		}
		addColumns(parsed, match[1], []string{column})
	}
	return parsed
}

// compareInventories reports tables or columns that exist on only one side.
func compareInventories(left, right inventory) []string {
	var issues []string
	for _, table := range unionKeys(left, right) {
		leftCols, leftOK := left[table]
		rightCols, rightOK := right[table]
		if !leftOK {
			issues = append(issues, "table "+table+" only on the right")
			continue
		}
		if !rightOK {
			issues = append(issues, "table "+table+" only on the left")
			continue
		}
		for _, column := range unionColumnKeys(leftCols, rightCols) {
			_, inLeft := leftCols[column]
			_, inRight := rightCols[column]
			if !inLeft {
				issues = append(issues, "column "+table+"."+column+" only on the right")
			}
			if !inRight {
				issues = append(issues, "column "+table+"."+column+" only on the left")
			}
		}
	}
	slices.Sort(issues)
	return issues
}

func upSection(sqlText string) string {
	if index := strings.Index(sqlText, "-- +goose Down"); index >= 0 {
		return sqlText[:index]
	}
	return sqlText
}

func addColumns(parsed inventory, table string, columns []string) {
	set := parsed[table]
	if set == nil {
		set = map[string]struct{}{}
		parsed[table] = set
	}
	for _, column := range columns {
		set[column] = struct{}{}
	}
}

func columnNames(body string) []string {
	var names []string
	for _, part := range splitComma(body) {
		field := strings.TrimSpace(part)
		if field == "" {
			continue
		}
		name := strings.Fields(field)[0]
		if isTableConstraint(name) {
			continue
		}
		names = append(names, name)
	}
	return names
}

func splitComma(body string) []string {
	var parts []string
	start := 0
	depth := 0
	inQuote := false
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\'':
			if inQuote && i+1 < len(body) && body[i+1] == '\'' {
				i++
				continue
			}
			inQuote = !inQuote
		case '(':
			if !inQuote {
				depth++
			}
		case ')':
			if !inQuote && depth > 0 {
				depth--
			}
		case ',':
			if !inQuote && depth == 0 {
				parts = append(parts, body[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, body[start:])
}

func matchingParen(sqlText string, open int) (string, int) {
	depth := 0
	inQuote := false
	for i := open; i < len(sqlText); i++ {
		switch sqlText[i] {
		case '\'':
			if inQuote && i+1 < len(sqlText) && sqlText[i+1] == '\'' {
				i++
				continue
			}
			inQuote = !inQuote
		case '(':
			if !inQuote {
				depth++
			}
		case ')':
			if inQuote {
				continue
			}
			depth--
			if depth == 0 {
				return sqlText[open+1 : i], i
			}
		}
	}
	return sqlText[open+1:], len(sqlText)
}

func isTableConstraint(name string) bool {
	switch strings.ToUpper(name) {
	case "PRIMARY", "UNIQUE", "CHECK", "FOREIGN", "CONSTRAINT", "EXCLUDE":
		return true
	default:
		return false
	}
}

// stripComments drops -- and /* */ comments that are outside a quoted string.
// A comment is replaced with a space so adjacent tokens stay separate.
func stripComments(sqlText string) string {
	var b strings.Builder
	b.Grow(len(sqlText))
	inQuote := false
	for i := 0; i < len(sqlText); i++ {
		if !inQuote {
			if next, ok := skipComment(sqlText, i); ok {
				b.WriteByte(' ')
				i = next - 1
				continue
			}
		}
		b.WriteByte(sqlText[i])
		if sqlText[i] != '\'' {
			continue
		}
		if inQuote && i+1 < len(sqlText) && sqlText[i+1] == '\'' {
			b.WriteByte('\'')
			i++
			continue
		}
		inQuote = !inQuote
	}
	return b.String()
}

// skipComment reports where the scanner resumes. A line comment stops on its newline.
// A block comment stops after the closing marker.
func skipComment(sqlText string, i int) (int, bool) {
	if i+1 >= len(sqlText) {
		return i, false
	}
	if sqlText[i] == '-' && sqlText[i+1] == '-' {
		newline := strings.IndexByte(sqlText[i+2:], '\n')
		if newline < 0 {
			return len(sqlText), true
		}
		return i + 2 + newline, true
	}
	if sqlText[i] == '/' && sqlText[i+1] == '*' {
		end := strings.Index(sqlText[i+2:], "*/")
		if end < 0 {
			return len(sqlText), true
		}
		return i + 2 + end + 2, true
	}
	return i, false
}

func unionKeys(left, right inventory) []string {
	seen := map[string]struct{}{}
	for name := range left {
		seen[name] = struct{}{}
	}
	for name := range right {
		seen[name] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func unionColumnKeys(left, right map[string]struct{}) []string {
	seen := map[string]struct{}{}
	for name := range left {
		seen[name] = struct{}{}
	}
	for name := range right {
		seen[name] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
