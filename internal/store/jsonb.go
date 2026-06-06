package store

import "encoding/json"

// jsonText marshals a value to JSON text for SQLite TEXT columns.
func jsonText(v any) *string {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}
