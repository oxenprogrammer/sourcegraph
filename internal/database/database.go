package database

import (
	"github.com/sourcegraph/sourcegraph/internal/database/dbutil"
)

// DB is an interface that embeds dbutil.DB, adding methods to
// return specialized stores on top of that interface. In time,
// the expectation is to replace uses of dbutil.DB with database.DB,
// and remove dbutil.DB altogether.
type DB interface {
	dbutil.DB
	Repos() RepoStore
}

// NewDB creates a new DB from a dbutil.DB, providing a thin wrapper
// that has constructor methods for the more specialized stores.
func NewDB(inner dbutil.DB) DB {
	return &db{inner}
}

type db struct {
	dbutil.DB
}

func (d *db) Repos() RepoStore {
	return Repos(d.DB)
}
