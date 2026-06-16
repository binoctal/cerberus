package store

import (
	"database/sql"
)

// nullString converts sql.NullString to interface{} for database queries
func nullString(ns sql.NullString) interface{} {
	if ns.Valid {
		return ns.String
	}
	return nil
}

// nullTime converts sql.NullTime to interface{} for database queries
func nullTime(nt sql.NullTime) interface{} {
	if nt.Valid {
		return nt.Time
	}
	return nil
}

// nullFloat64 converts sql.NullFloat64 to interface{} for database queries
func nullFloat64(nf sql.NullFloat64) interface{} {
	if nf.Valid {
		return nf.Float64
	}
	return nil
}
