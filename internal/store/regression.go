package store

// NewRegressionStore creates a new regression store
func NewRegressionStore(store *Store) *RegressionStore {
	return &RegressionStore{store: store}
}
