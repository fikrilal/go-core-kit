//go:build !pg

package config_test

// laneDatabaseEnv is the key and value the compiled lane requires.
func laneDatabaseEnv() (string, string) {
	return "DATABASE_PATH", "./.data/test.db"
}
