package httpx_test

import "github.com/fikrilal/go-core-kit/internal/platform/httpx"

func testOp(method, path string) httpx.Operation {
	return httpx.Operation{Method: method, Path: path, ID: "test:" + method + ":" + path}
}
