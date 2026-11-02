//go:build cgo

package app

import (
	"errors"

	"github.com/mattn/go-sqlite3"
)

func isRetryableStoreError(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked)
}
