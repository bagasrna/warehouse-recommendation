package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"example.com/simpul-warehouse-selection/internal/domain"
)

// Load validates and canonicalizes a read-only catalog before serving traffic.
func Load(r io.Reader) (domain.Catalog, error) {
	var c domain.Catalog
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("decode catalog: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("catalog must contain exactly one JSON value")
	}
	if len(c.Products) == 0 || len(c.Warehouses) == 0 {
		return c, fmt.Errorf("catalog requires products and warehouses")
	}
	products := make(map[string]bool, len(c.Products))
	for _, p := range c.Products {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Category) == "" || products[p.ID] {
			return c, fmt.Errorf("invalid or duplicate product %q", p.ID)
		}
		products[p.ID] = true
	}
	warehouses := make(map[string]bool, len(c.Warehouses))
	for _, w := range c.Warehouses {
		if strings.TrimSpace(w.ID) == "" || strings.TrimSpace(w.City) == "" || w.DistanceKM < 0 || warehouses[w.ID] {
			return c, fmt.Errorf("invalid or duplicate warehouse %q", w.ID)
		}
		warehouses[w.ID] = true
		for id, qty := range w.Stock {
			if !products[id] || qty < 0 {
				return c, fmt.Errorf("invalid stock for warehouse %s and product %s", w.ID, id)
			}
		}
	}
	sort.Slice(c.Products, func(i, j int) bool { return c.Products[i].ID < c.Products[j].ID })
	sort.Slice(c.Warehouses, func(i, j int) bool { return c.Warehouses[i].ID < c.Warehouses[j].ID })
	return c, nil
}
