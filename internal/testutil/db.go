package testutil

import (
	"database/sql"
	"testing"

	"github.com/grantdeshazer/build-server/internal/db"
)

// OpenTestDB opens an in-memory SQLite DB, runs migrations, and registers
// t.Cleanup to close it.
func OpenTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("OpenTestDB: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}
