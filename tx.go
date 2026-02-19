package mysql

import (
	"fmt"
	"runtime/debug"
)

// Transaction runs fn in a transaction.
//
// Behavior:
// - begin transaction
// - if fn returns error -> rollback and return that error
// - if fn panics -> rollback and return an error containing the panic and stack
// - if fn succeeds -> commit
//
// The SQLBuilder is always returned to the pool via Free().
func (m *DbModel) Transaction(fn func(sb *SQLBuilder) error) (err error) {
	if m == nil {
		return fmt.Errorf("db model not initialized")
	}
	sb := m.Orm()
	// Ensure the builder is always reclaimed.
	defer sb.Free()

	_, err = sb.Begin()
	if err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			_, _ = sb.Rollback()
			err = fmt.Errorf("transaction panic: %v\n%s", r, debug.Stack())
			return
		}
		if err != nil {
			_, _ = sb.Rollback()
			return
		}
		_, err = sb.Commit()
	}()

	err = fn(sb)
	return err
}
