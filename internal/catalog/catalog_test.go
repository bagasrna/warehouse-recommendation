package catalog

import (
	"strings"
	"testing"
)

func TestInvalidCatalog(t *testing.T) {
	cases := []string{
		`{}`,
		`{"products":[{"id":"P001","name":"Product","category":"Medical"}],"warehouses":[{"id":"W01","city":"City","distance_km":-1,"stock":{"P001":1}}]}`,
		`{"products":[{"id":"P001","name":"Product","category":"Medical"}],"warehouses":[{"id":"W01","city":"City","distance_km":5,"stock":{"P002":1}}]}`,
		`{"products":[{"id":"P001","name":"Product","category":"Medical"}],"warehouses":[{"id":"W01","city":"City","distance_km":5,"stock":{"P001":-1}}]}`,
		`{"products":[],"warehouses":[]}`,
	}
	for _, data := range cases {
		if _, err := Load(strings.NewReader(data)); err == nil {
			t.Fatalf("expected failure for %s", data)
		}
	}
}
