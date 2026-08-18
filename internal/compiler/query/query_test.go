package query

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_Ok(t *testing.T) {
	jsonStr := `{
		"measures": ["orders.count"],
		"dimensions": ["orders.status"],
		"timeDimensions": [{
			"dimension": "orders.created_at",
			"dateRange": ["2026-08-01", "2026-08-15"],
			"granularity": "day"
		}],
		"filters": [
			{"member": "orders.status", "operator": "equals", "values": ["paid"]}
		],
		"limit": 100
	}`
	q, err := Parse([]byte(jsonStr))
	require.NoError(t, err)
	assert.Equal(t, []string{"orders.count"}, q.Measures)
	assert.Equal(t, []string{"orders.status"}, q.Dimensions)
	require.Len(t, q.TimeDimensions, 1)
	assert.Equal(t, "day", q.TimeDimensions[0].Granularity)
	require.Len(t, q.Filters, 1)
	assert.Equal(t, "equals", q.Filters[0].Operator)
	require.NotNil(t, q.Limit)
	assert.Equal(t, 100, *q.Limit)
}

func TestParse_Errors(t *testing.T) {
	cases := []struct {
		name  string
		input string
		field string
	}{
		{
			name:  "empty",
			input: `{}`,
			field: "measures/dimensions",
		},
		{
			name:  "bad measure format",
			input: `{"measures": ["count"]}`,
			field: "measures",
		},
		{
			name:  "bad operator",
			input: `{"measures": ["o.c"], "filters": [{"member": "o.x", "operator": "foo", "values": []}]}`,
			field: "filters[0].operator",
		},
		{
			name:  "bad granularity",
			input: `{"measures": ["o.c"], "timeDimensions": [{"dimension": "o.d", "granularity": "second"}]}`,
			field: "timeDimensions[0].granularity",
		},
		{
			name:  "limit too big",
			input: `{"measures": ["o.c"], "limit": 99999}`,
			field: "limit",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.input))
			require.Error(t, err)
			pe, ok := err.(*ParseError)
			require.True(t, ok, "expected *ParseError, got %T", err)
			assert.Contains(t, pe.Field, c.field)
		})
	}
}

func TestParse_DefaultLimit(t *testing.T) {
	q, err := Parse([]byte(`{"measures": ["o.c"]}`))
	require.NoError(t, err)
	require.NotNil(t, q.Limit)
	assert.Equal(t, 10000, *q.Limit)
}

func TestReferencedCubes(t *testing.T) {
	q, err := Parse([]byte(`{
		"measures": ["orders.count", "products.count"],
		"dimensions": ["orders.status", "products.category"],
		"timeDimensions": [{"dimension": "orders.created_at"}],
		"filters": [{"member": "users.id", "operator": "equals", "values": [1]}]
	}`))
	require.NoError(t, err)
	cubes := q.ReferencedCubes()
	assert.Len(t, cubes, 3)
	assert.Contains(t, cubes, "orders")
	assert.Contains(t, cubes, "products")
	assert.Contains(t, cubes, "users")
}
