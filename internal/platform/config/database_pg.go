//go:build pg

package config

import "fmt"

// validateDatabase requires the Postgres lane's connection string. The pg build
// ignores DATABASE_PATH entirely, so demanding it here would make the lane
// unbootable without a value it never reads.
func (c Config) validateDatabase() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required for the pg build")
	}
	return nil
}
