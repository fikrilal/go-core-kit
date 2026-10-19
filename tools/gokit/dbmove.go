package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/db/utctime"
	"github.com/fikrilal/go-core-kit/internal/platform/dblane"
)

// dbMoveTables is the kit's schema in dependency order: parents before children, so
// an import never inserts a row whose reference is missing. A future migration adds
// its table here — the snapshot format does not discover tables, because portable
// discovery would have to name an engine.
var dbMoveTables = []string{
	"users",
	"password_credentials",
	"external_identities",
	"stored_files",
	"user_profiles",
	"sessions",
	"refresh_tokens",
	"email_verification_tokens",
	"password_reset_tokens",
	"user_account_deletion_audits",
}

// dbMoveSelfReferences names the columns that point back at their own table. They are
// inserted null and filled in a second pass, because a row's parent may follow it in
// the snapshot.
var dbMoveSelfReferences = map[string]string{
	"refresh_tokens": "replaced_by_id",
}

// dbMoveHeader opens a table in the snapshot; each following line is one row.
type dbMoveHeader struct {
	Version int      `json:"version"`
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
}

// dbMoveTable is one table's snapshot: its columns and its rows, values as stored
// text or null.
type dbMoveTable struct {
	columns []string
	rows    [][]*string
}

type dbExportCommand struct {
	out string
}

func (c dbExportCommand) execute(ctx context.Context) error {
	database, err := openCompiledLane()
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	return runDBExport(ctx, database, c.out)
}

type dbImportCommand struct {
	in      string
	replace bool
	dryRun  bool
}

func (c dbImportCommand) execute(ctx context.Context) error {
	database, err := openCompiledLane()
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	return runDBImport(ctx, database, c.in, c.replace, c.dryRun)
}

// runDBExport writes every table of the compiled lane to out as JSON lines.
func runDBExport(ctx context.Context, database *db.Database, out string) error {
	temp := out + ".tmp"
	file, err := os.Create(temp) //nolint:gosec // the operator names the output path
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	writer := bufio.NewWriter(file)
	// A failed export must not leave a partial file beside the destination.
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temp)
		}
	}()
	exported := 0
	for _, table := range dbMoveTables {
		count, err := exportTable(ctx, database, writer, table)
		if err != nil {
			_ = file.Close()
			return err
		}
		exported += count
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return fmt.Errorf("flush snapshot: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close snapshot: %w", err)
	}
	if err := os.Rename(temp, out); err != nil {
		return fmt.Errorf("publish snapshot: %w", err)
	}
	committed = true
	fmt.Printf("export: %d rows from %d tables into %s\n", exported, len(dbMoveTables), out)
	return nil
}

// exportTable streams one table: its header line, then one line per row.
func exportTable(ctx context.Context, database *db.Database, writer *bufio.Writer, table string) (int, error) {
	rows, err := database.SQL().QueryContext(ctx, "SELECT * FROM "+table)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("columns for %s: %w", table, err)
	}
	if err := writeSnapshotLine(writer, dbMoveHeader{Version: 1, Table: table, Columns: columns}); err != nil {
		return 0, err
	}
	count := 0
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return 0, fmt.Errorf("scan %s: %w", table, err)
		}
		row := make([]*string, len(columns))
		for i, value := range values {
			row[i] = snapshotValue(value)
		}
		if err := writeSnapshotLine(writer, row); err != nil {
			return 0, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read %s: %w", table, err)
	}
	return count, nil
}

// snapshotValue renders a scanned value as the text both engines store, or null.
// Timestamps take the canonical stored form: the pg driver hands them back as
// time.Time, and %v would write the operator's local zone — text the pg lanes reject
// and this kit's readers cannot parse.
func snapshotValue(value any) *string {
	text := ""
	switch typed := value.(type) {
	case nil:
		return nil
	case []byte:
		text = string(typed)
	case string:
		text = typed
	case time.Time:
		text = utctime.Time{Time: typed}.Stored()
	case int64:
		text = strconv.FormatInt(typed, 10)
	case float64:
		text = strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		text = strconv.FormatBool(typed)
	default:
		text = fmt.Sprintf("%v", typed)
	}
	return &text
}

func writeSnapshotLine(writer *bufio.Writer, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode snapshot line: %w", err)
	}
	if _, err := writer.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

// runDBImport applies a snapshot to the compiled lane.
func runDBImport(ctx context.Context, database *db.Database, in string, replace, dryRun bool) error {
	snapshot, err := readSnapshot(in)
	if err != nil {
		return err
	}
	if dryRun {
		total := 0
		for _, table := range dbMoveTables {
			total += len(snapshot[table].rows)
		}
		fmt.Printf("dry-run: %d rows across %d tables would be written\n", total, len(dbMoveTables))
		return nil
	}

	if !replace {
		held, err := heldTables(ctx, database)
		if err != nil {
			return err
		}
		if len(held) > 0 {
			return fmt.Errorf("target already holds rows in %s — pass --replace to overwrite", strings.Join(held, ", "))
		}
	}
	inserted, err := writeSnapshot(ctx, database, snapshot, replace)
	if err != nil {
		return err
	}
	fmt.Printf("import: %d rows into %d tables\n", inserted, len(dbMoveTables))
	return nil
}

// writeSnapshot applies the whole snapshot in one transaction: a failure anywhere
// leaves the target exactly as it was, which matters most for --replace, where the
// deletes would otherwise commit table by table and cascade rows away for good.
func writeSnapshot(ctx context.Context, database *db.Database, snapshot map[string]dbMoveTable, replace bool) (int, error) {
	// The count is assigned from the attempt that commits, not accumulated across
	// attempts: InTx may retry a serializable conflict and rerun this closure.
	inserted := 0
	err := database.InTx(ctx, func(tx db.Tx) error {
		attemptRows := 0
		defer func() { inserted = attemptRows }()
		if replace {
			for i := len(dbMoveTables) - 1; i >= 0; i-- {
				if _, err := tx.ExecContext(ctx, "DELETE FROM "+dbMoveTables[i]); err != nil {
					return fmt.Errorf("clear %s: %w", dbMoveTables[i], err)
				}
			}
		}
		for _, table := range dbMoveTables {
			count, deferred, err := insertRows(ctx, tx, table, snapshot[table])
			if err != nil {
				return err
			}
			attemptRows += count
			if err := applySelfReferences(ctx, tx, table, deferred); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("import: %w", err)
	}
	return inserted, nil
}

// insertRows writes one table's rows, returning the self-references it deferred.
func insertRows(ctx context.Context, tx db.Tx, table string, data dbMoveTable) (int, [][2]string, error) {
	if len(data.rows) == 0 {
		return 0, nil, nil
	}
	statement := db.Rebind(insertStatement(table, data.columns))
	selfColumn := dbMoveSelfReferences[table]
	var deferred [][2]string
	for _, row := range data.rows {
		values := make([]any, len(row))
		for i, value := range row {
			if value == nil {
				continue
			}
			if selfColumn != "" && data.columns[i] == selfColumn {
				idIndex := slices.Index(data.columns, "id")
				if idIndex < 0 || row[idIndex] == nil {
					return 0, nil, fmt.Errorf("row in %s has no id to link", table)
				}
				// The link is filled in once every row exists, keyed by the row's own
				// id wherever the snapshot put that column.
				deferred = append(deferred, [2]string{*row[idIndex], *value})
				continue
			}
			values[i] = *value
		}
		if _, err := tx.ExecContext(ctx, statement, values...); err != nil {
			return 0, nil, fmt.Errorf("insert into %s: %w", table, err)
		}
	}
	return len(data.rows), deferred, nil
}

// applySelfReferences fills in the columns deferred during insert.
func applySelfReferences(ctx context.Context, tx db.Tx, table string, deferred [][2]string) error {
	selfColumn := dbMoveSelfReferences[table]
	if selfColumn == "" || len(deferred) == 0 {
		return nil
	}
	statement := db.Rebind("UPDATE " + table + " SET " + selfColumn + " = ? WHERE id = ?")
	for _, pair := range deferred {
		if _, err := tx.ExecContext(ctx, statement, pair[1], pair[0]); err != nil {
			return fmt.Errorf("link %s.%s: %w", table, selfColumn, err)
		}
	}
	return nil
}

// readSnapshot parses the JSON lines into per-table columns and rows.
func readSnapshot(in string) (map[string]dbMoveTable, error) {
	file, err := os.Open(in) //nolint:gosec // the operator names the input path
	if err != nil {
		return nil, fmt.Errorf("open snapshot: %w", err)
	}
	defer func() { _ = file.Close() }()

	tables := map[string]dbMoveTable{}
	current := ""
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var header dbMoveHeader
		if err := json.Unmarshal([]byte(line), &header); err == nil && header.Table != "" {
			if !slices.Contains(dbMoveTables, header.Table) {
				return nil, fmt.Errorf("snapshot names a table this kit does not carry: %s", header.Table)
			}
			current = header.Table
			tables[current] = dbMoveTable{columns: header.Columns}
			continue
		}
		if current == "" {
			return nil, fmt.Errorf("snapshot line does not follow a table header: %.60s", line)
		}
		var row []*string
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("decode %s row: %w", current, err)
		}
		table := tables[current]
		if len(row) != len(table.columns) {
			return nil, fmt.Errorf("row in %s has %d values for %d columns", current, len(row), len(table.columns))
		}
		table.rows = append(table.rows, row)
		tables[current] = table
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	return tables, nil
}

// heldTables reports which managed tables already hold rows. Any of them triggers the
// --replace requirement, not only the ones the snapshot would touch.
func heldTables(ctx context.Context, database *db.Database) ([]string, error) {
	var held []string
	for _, table := range dbMoveTables {
		var count int64
		if err := database.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			return nil, fmt.Errorf("count %s: %w", table, err)
		}
		if count > 0 {
			held = append(held, table)
		}
	}
	return held, nil
}

// insertStatement builds an insert with the portable placeholder form.
func insertStatement(table string, columns []string) string {
	placeholders := make([]string, len(columns))
	for i := range columns {
		placeholders[i] = "?"
	}
	return "INSERT INTO " + table + " (" + strings.Join(columns, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
}

// openCompiledLane opens the database the compiled engine uses.
func openCompiledLane() (*db.Database, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	database, err := dblane.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("open the compiled lane: %w", err)
	}
	return database, nil
}
