package sqlite_test

import (
	"testing"

	"github.com/optimuspaul/personal-oplog/internal/persistence"
	"github.com/optimuspaul/personal-oplog/internal/persistence/sqlite"
	"github.com/optimuspaul/personal-oplog/internal/persistence/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.NewStore {
		dir := t.TempDir()
		return func() persistence.Store {
			s, err := sqlite.NewSqliteStore(dir)
			if err != nil {
				t.Fatalf("NewSqliteStore: %v", err)
			}
			return s
		}
	})
}
