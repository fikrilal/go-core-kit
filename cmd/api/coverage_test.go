//go:build !pg

package main

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/fikrilal/go-core-kit/contracts"
	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

func TestContractCoverage(t *testing.T) {
	suite := contracttest.New(t)
	registered := coverageRouter(t).Operations()
	pending := loadCoverageAllowlist(t)
	snapshot := map[string]contracttest.SnapshotOperation{}
	for _, op := range suite.SnapshotOperations() {
		if _, duplicate := snapshot[op.ID]; duplicate {
			t.Fatalf("snapshot declares operation %q twice", op.ID)
		}
		snapshot[op.ID] = op
	}

	checkRegistrations(t, snapshot, pending, registered)
	checkMissing(t, snapshot, pending, registered)
	for id := range pending {
		if _, ok := snapshot[id]; !ok {
			t.Errorf("allowlisted operation %q is not in the snapshot", id)
		}
	}
}

func coverageRouter(t *testing.T) *httpx.Router {
	t.Helper()
	database := db.MustOpen(filepath.Join(t.TempDir(), "coverage.db"))
	t.Cleanup(func() { _ = database.Close() })
	rdb := redis.MustOpen("redis://127.0.0.1:1/0")
	t.Cleanup(func() { _ = rdb.Close() })
	logger := logging.New(config.LogLevelError, config.LogFormatJSON, io.Discard)
	server, err := newServer(testConfig(t), logger, database, rdb)
	if err != nil {
		t.Fatalf("newServer() error = %v", err)
	}
	router, ok := server.Handler.(*httpx.Router)
	if !ok {
		t.Fatal("server handler is not an *httpx.Router")
	}
	return router
}

func loadCoverageAllowlist(t *testing.T) map[string]bool {
	t.Helper()
	rawAllowlist, err := os.ReadFile(contracttest.RepositoryPath(t, "contracts/allowlist.yaml"))
	if err != nil {
		t.Fatalf("read allowlist: %v", err)
	}
	pending := map[string]bool{}
	for _, id := range contracts.ParseAllowlist(rawAllowlist) {
		if pending[id] {
			t.Errorf("allowlist lists %q twice", id)
		}
		pending[id] = true
	}
	return pending
}

func checkRegistrations(t *testing.T, snapshot map[string]contracttest.SnapshotOperation, pending map[string]bool, registered []httpx.Operation) {
	t.Helper()
	for _, op := range registered {
		if declared, ok := snapshot[op.ID]; !ok {
			t.Errorf("registered operation %q is not in the snapshot", op.ID)
		} else if declared.Method != op.Method || declared.Path != op.Path {
			t.Errorf("operation %q registered as %s %s, snapshot declares %s %s", op.ID, op.Method, op.Path, declared.Method, declared.Path)
		}
		if pending[op.ID] {
			t.Errorf("operation %q is registered but still in the allowlist", op.ID)
		}
	}
}

func checkMissing(t *testing.T, snapshot map[string]contracttest.SnapshotOperation, pending map[string]bool, registered []httpx.Operation) {
	t.Helper()
	implemented := map[string]bool{}
	for _, op := range registered {
		implemented[op.ID] = true
	}
	var missing []string
	for id := range snapshot {
		if !implemented[id] && !pending[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		t.Errorf("snapshot operations missing from the allowlist and not implemented: %v", missing)
	}
}
