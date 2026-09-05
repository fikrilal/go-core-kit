package httpx_test

import (
	"bytes"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
)

func testRouter() (*httpx.Router, *bytes.Buffer) {
	var logs bytes.Buffer
	logger := logging.New(config.LogLevelDebug, config.LogFormatJSON, &logs)
	return httpx.NewRouter(logger), &logs
}
