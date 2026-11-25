package dbgen

// Connection exposes the transaction-bound query connection to trusted policy
// providers that use their own sqlc package.
func (q *Queries) Connection() DBTX { return q.db }
