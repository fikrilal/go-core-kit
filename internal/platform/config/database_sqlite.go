//go:build !pg

package config

import "fmt"

// validateDatabase requires the SQLite lane's database path.
func (c Config) validateDatabase() error {
	if c.DatabasePath == "" {
		return fmt.Errorf("DATABASE_PATH is required for the sqlite build")
	}
	return nil
}
