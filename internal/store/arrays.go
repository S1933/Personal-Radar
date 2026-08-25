package store

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"

	"github.com/lib/pq"
)

// pqArray returns a pq.StringArray wrapping dst for scanning TEXT[] columns.
func pqArray(dst *[]string) interface {
	Scan(any) error
} {
	return (*pq.StringArray)(dst)
}

// int64Array converts []int64 to a pq driver value for BIGINT[] columns.
func int64Array(v []int64) driver.Valuer { return pq.Array(v) }

// scanInt64Array parses a Postgres array literal into []int64 (used for
// tests and lightweight reads).
func scanInt64Array(s string) ([]int64, error) {
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse int64 array: %w", err)
		}
		out = append(out, n)
	}
	return out, nil
}

var _ = scanInt64Array // used by tests
