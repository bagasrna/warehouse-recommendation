package selection

import (
	"errors"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"example.com/simpul-warehouse-selection/internal/assets"
	"example.com/simpul-warehouse-selection/internal/domain"
)

func testEngine(t testing.TB) *Engine {
	t.Helper()
	c, err := assets.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(c, DefaultPenaltyKMEq)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func order(id string, qty int64) []domain.OrderItem {
	return []domain.OrderItem{{ProductID: id, Quantity: qty}}
}

func TestGoldenScenarios(t *testing.T) {
	e := testEngine(t)
	cases := []struct {
		name       string
		items      []domain.OrderItem
		wantStatus string
		ids        []string
		score      int
	}{
		{"T01-gloves-250", order("P001", 250), "FULFILLABLE", []string{"W03"}, 25},
		{"T02-gloves-400", order("P001", 400), "FULFILLABLE", []string{"W01", "W03"}, 50},
		{"T03-gloves-600-tie", order("P001", 600), "FULFILLABLE", []string{"W01", "W05"}, 115},
		{"T04-hepb-40", order("P002", 40), "FULFILLABLE", []string{"W04"}, 30},
		{"T05-hepb-45", order("P002", 45), "FULFILLABLE", []string{"W04", "W07"}, 95},
		{"T06-hepb-48", order("P002", 48), "FULFILLABLE", []string{"W04", "W07"}, 95},
		{"T07-hepb-49", order("P002", 49), "FULFILLABLE", []string{"W08"}, 150},
		{"T08-hepb-68", order("P002", 68), "FULFILLABLE", []string{"W04", "W08"}, 200},
		{"T09-hepb-108", order("P002", 108), "FULFILLABLE", []string{"W04", "W07", "W08"}, 265},
		{"T10-hepb-109", order("P002", 109), "INSUFFICIENT_ELIGIBLE_STOCK", nil, 0},
		{"T11-tetanus-86", order("P004", 86), "INSUFFICIENT_ELIGIBLE_STOCK", nil, 0},
		{"T12-paracetamol-301", order("P005", 301), "FULFILLABLE", []string{"W01", "W04"}, 55},
		{"T13-multi-sku", []domain.OrderItem{{ProductID: "P001", Quantity: 250}, {ProductID: "P003", Quantity: 300}}, "FULFILLABLE", []string{"W01", "W02"}, 37},
		{"T14-multi-sku-split", []domain.OrderItem{{ProductID: "P001", Quantity: 250}, {ProductID: "P003", Quantity: 501}}, "FULFILLABLE", []string{"W01", "W03"}, 50},
		{"T15-multi-vaccines", []domain.OrderItem{{ProductID: "P002", Quantity: 61}, {ProductID: "P004", Quantity: 51}}, "FULFILLABLE", []string{"W04", "W08"}, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := e.Recommend(tc.items)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != tc.wantStatus {
				t.Fatalf("status %s, expected %s", result.Status, tc.wantStatus)
			}
			if result.Recommendation == nil {
				if tc.ids != nil {
					t.Fatal("nil recommendation")
				}
				if len(result.Shortages) == 0 {
					t.Fatal("missing shortages")
				}
				return
			}
			if result.Recommendation.ScoreKMEq != tc.score {
				t.Errorf("score %d, want %d", result.Recommendation.ScoreKMEq, tc.score)
			}
			var ids []string
			for _, a := range result.Recommendation.Warehouses {
				ids = append(ids, a.WarehouseID)
			}
			if !reflect.DeepEqual(ids, tc.ids) {
				t.Errorf("warehouses %v, want %v", ids, tc.ids)
			}
		})
	}
}

func TestColdChainAndInsufficient(t *testing.T) {
	e := testEngine(t)
	result, err := e.Recommend(order("P002", 109))
	if err != nil {
		t.Fatal(err)
	}
	if result.Recommendation != nil {
		t.Fatal("partial allocation must not be returned")
	}
	s := result.Shortages[0]
	if s.PhysicalStock != 158 || s.EligibleStock != 108 || s.ShortageQuantity != 1 {
		t.Fatalf("unexpected shortages: %+v", s)
	}
	r, err := e.Recommend(order("P002", 108))
	if err != nil || r.Status != "FULFILLABLE" {
		t.Fatalf("108 should be feasible: %v %v", r.Status, err)
	}
	for _, a := range r.Recommendation.Warehouses {
		if a.WarehouseID == "W02" || a.WarehouseID == "W06" {
			t.Fatalf("cold-chain violating source: %s", a.WarehouseID)
		}
	}
}

func TestNormalizeAndValidation(t *testing.T) {
	e := testEngine(t)
	cases := [][]domain.OrderItem{
		nil, {}, order("MISSING", 1), order("P001", 0), order("P001", -3),
	}
	for _, items := range cases {
		_, err := e.Recommend(items)
		if !errors.Is(err, ErrInvalidOrder) {
			t.Errorf("expected invalid input for %v, got %v", items, err)
		}
	}
	a, err := e.Recommend(order("P001", 250))
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Recommend([]domain.OrderItem{{ProductID: "P001", Quantity: 100}, {ProductID: "P001", Quantity: 150}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("duplicate SKU normalization changed result")
	}
}

func TestOrderIndependenceAndImmutableCatalog(t *testing.T) {
	c, err := assets.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	e := testEngine(t)
	items := []domain.OrderItem{{ProductID: "P003", Quantity: 501}, {ProductID: "P001", Quantity: 250}, {ProductID: "P002", Quantity: 35}, {ProductID: "P004", Quantity: 15}}
	initial, err := e.Recommend(items)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		r := rand.New(rand.NewSource(int64(i)))
		permuted := append([]domain.OrderItem(nil), items...)
		r.Shuffle(len(permuted), func(i, j int) { permuted[i], permuted[j] = permuted[j], permuted[i] })
		got, err := e.Recommend(permuted)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, initial) {
			t.Fatal("order affects output")
		}
	}
	r := rand.New(rand.NewSource(123))
	r.Shuffle(len(c.Warehouses), func(i, j int) { c.Warehouses[i], c.Warehouses[j] = c.Warehouses[j], c.Warehouses[i] })
	r.Shuffle(len(c.Products), func(i, j int) { c.Products[i], c.Products[j] = c.Products[j], c.Products[i] })
	other, err := New(c, 20)
	if err != nil {
		t.Fatal(err)
	}
	got, err := other.Recommend(items)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, initial) {
		t.Fatal("warehouse order affects output")
	}
	before := c.Warehouses[0].Stock["P002"]
	c.Warehouses[0].Stock["P002"] = 99999 // mutation of caller data must not affect engine
	after, err := other.Recommend(items)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, initial) {
		t.Fatal("catalog was not defensively copied")
	}
	c.Warehouses[0].Stock["P002"] = before
}

func TestRandomInvariants(t *testing.T) {
	e := testEngine(t)
	ids := []string{"P001", "P002", "P003", "P004", "P005"}
	r := rand.New(rand.NewSource(901))
	for k := 0; k < 400; k++ {
		items := make([]domain.OrderItem, 0, 5)
		for _, id := range ids {
			if r.Intn(3) != 0 {
				items = append(items, domain.OrderItem{ProductID: id, Quantity: int64(1 + r.Intn(1200))})
			}
		}
		if len(items) == 0 {
			items = order("P001", 1)
		}
		res, err := e.Recommend(items)
		if err != nil {
			t.Fatalf("iteration %d, input %v: %v", k, items, err)
		}
		if res.Recommendation == nil {
			if len(res.Shortages) == 0 {
				t.Fatalf("insufficient status without shortage")
			}
			continue
		}
		normalized, err := e.normalize(items)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.verify(res.Recommendation, normalized); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTieBreakerProductSplitAndWarehouseID(t *testing.T) {
	// Equal score/count; lower product split should win.
	c := domain.Catalog{
		Products: []domain.Product{{ID: "P001"}, {ID: "P002"}},
		Warehouses: []domain.Warehouse{
			{ID: "W04", DistanceKM: 9, Stock: map[string]int64{"P001": 1, "P002": 5}},
			{ID: "W02", DistanceKM: 1, Stock: map[string]int64{"P001": 5, "P002": 0}},
			{ID: "W03", DistanceKM: 1, Stock: map[string]int64{"P001": 0, "P002": 5}},
		},
	}
	e, err := New(c, 20)
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.Recommend([]domain.OrderItem{{ProductID: "P001", Quantity: 5}, {ProductID: "P002", Quantity: 5}})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, w := range result.Recommendation.Warehouses {
		ids = append(ids, w.WarehouseID)
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"W02", "W03"}) {
		t.Fatalf("unexpected %v", ids)
	}
}

func BenchmarkSelection(b *testing.B) {
	e := testEngine(b)
	cases := map[string][]domain.OrderItem{
		"one_sku_one_warehouse":       order("P002", 40),
		"one_sku_multiple_warehouses": order("P001", 600),
		"several_skus":                {{ProductID: "P001", Quantity: 250}, {ProductID: "P003", Quantity: 501}, {ProductID: "P002", Quantity: 45}},
		"insufficient":                order("P002", 109),
	}
	for name, items := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := e.Recommend(items); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestMinimizeProductSplitOnBalancedScoreTie(t *testing.T) {
	c := domain.Catalog{
		Products: []domain.Product{{ID: "P001"}, {ID: "P002"}},
		Warehouses: []domain.Warehouse{
			{ID: "W01", DistanceKM: 10, Stock: map[string]int64{"P001": 4, "P002": 4}},
			{ID: "W02", DistanceKM: 10, Stock: map[string]int64{"P001": 4, "P002": 4}},
			{ID: "W03", DistanceKM: 10, Stock: map[string]int64{"P001": 6, "P002": 2}},
			{ID: "W04", DistanceKM: 10, Stock: map[string]int64{"P001": 2, "P002": 6}},
		},
	}
	e, err := New(c, 20)
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.Recommend([]domain.OrderItem{{ProductID: "P001", Quantity: 6}, {ProductID: "P002", Quantity: 6}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Recommendation.ScoreKMEq != 40 {
		t.Fatal("expected balanced score 40")
	}
	var chosen []string
	for _, a := range result.Recommendation.Warehouses {
		chosen = append(chosen, a.WarehouseID)
	}
	if !reflect.DeepEqual(chosen, []string{"W03", "W04"}) || result.Recommendation.ProductSplitCount != 0 {
		t.Fatalf("lower product split should beat lexicographically earlier warehouse IDs: %v, splits=%d", chosen, result.Recommendation.ProductSplitCount)
	}
}

func TestEqualScoreLexicographicalTie(t *testing.T) {
	c := domain.Catalog{
		Products: []domain.Product{{ID: "P001"}},
		Warehouses: []domain.Warehouse{
			{ID: "W03", DistanceKM: 10, Stock: map[string]int64{"P001": 5}},
			{ID: "W01", DistanceKM: 10, Stock: map[string]int64{"P001": 5}},
		},
	}
	e, err := New(c, 20)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Recommend(order("P001", 5))
	if err != nil {
		t.Fatal(err)
	}
	if r.Recommendation.Warehouses[0].WarehouseID != "W01" {
		t.Fatal("ID tie breaker not deterministic")
	}
}

func TestConcurrentReadOnlyRecommendations(t *testing.T) {
	e := testEngine(t)
	items := []domain.OrderItem{{ProductID: "P001", Quantity: 250}, {ProductID: "P003", Quantity: 501}, {ProductID: "P002", Quantity: 45}}
	want, err := e.Recommend(items)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	const repeats = 50
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			for j := 0; j < repeats; j++ {
				got, err := e.Recommend(items)
				if err != nil {
					results <- err
					return
				}
				if !reflect.DeepEqual(got, want) {
					results <- errors.New("inconsistent concurrent recommendation")
					return
				}
			}
			results <- nil
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}
