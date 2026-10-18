//go:build pg

package config_test

// laneDatabaseEnv is the key and value the compiled lane requires.
func laneDatabaseEnv() (string, string) {
	return "DATABASE_URL", "postgres://postgres@localhost:5432/gokit?sslmode=disable"
}
