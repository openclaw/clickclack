package sqlite

import (
	"database/sql/driver"
	"fmt"
	"strings"

	modernsqlite "modernc.org/sqlite"
)

func init() {
	modernsqlite.MustRegisterDeterministicScalarFunction(
		"clickclack_lower",
		1,
		func(_ *modernsqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			switch value := args[0].(type) {
			case nil:
				return nil, nil
			case string:
				return strings.ToLower(value), nil
			case []byte:
				return strings.ToLower(string(value)), nil
			default:
				return nil, fmt.Errorf("clickclack_lower requires text, got %T", value)
			}
		},
	)
}
