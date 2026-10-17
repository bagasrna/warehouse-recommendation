// Package selection implements deterministic, read-only warehouse recommendations.
package selection

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"strings"

	"example.com/simpul-warehouse-selection/internal/domain"
	"example.com/simpul-warehouse-selection/internal/insights"
)

const DefaultPenaltyKMEq = 20

// PenaltyKMEq returns the configured additional-warehouse dispatch penalty.
func (e *Engine) PenaltyKMEq() int { return e.penalty }

var ErrInvalidOrder = errors.New("invalid order")

// ValidationError is an input problem, not a failure of the selection algorithm.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string        { return e.Field + ": " + e.Message }
func (e *ValidationError) Is(target error) bool { return target == ErrInvalidOrder }

// Engine uses a read-only, private copy of the catalog's stock data.
// All requests can safely share one engine without locks.
type Engine struct {
	products   map[string]domain.Product
	warehouses []domain.Warehouse
	penalty    int
}

func New(c domain.Catalog, penalty int) (*Engine, error) {
	if penalty < 0 {
		return nil, fmt.Errorf("penalty must be nonnegative")
	}
	// The enumerator is intentionally scoped to a small PoC, not hundreds of warehouses.
	if len(c.Warehouses) == 0 || len(c.Warehouses) > 16 {
		return nil, fmt.Errorf("warehouse count must be between 1 and 16")
	}
	e := &Engine{products: make(map[string]domain.Product, len(c.Products)), penalty: penalty}
	for _, p := range c.Products {
		if p.ID == "" {
			return nil, fmt.Errorf("product ID must not be empty")
		}
		if _, exists := e.products[p.ID]; exists {
			return nil, fmt.Errorf("duplicate product ID %s", p.ID)
		}
		e.products[p.ID] = p
	}
	e.warehouses = make([]domain.Warehouse, len(c.Warehouses))
	seen := make(map[string]bool)
	for i, w := range c.Warehouses {
		if w.ID == "" || seen[w.ID] || w.DistanceKM < 0 {
			return nil, fmt.Errorf("invalid warehouse ID/distance for %s", w.ID)
		}
		seen[w.ID] = true
		copyStock := make(map[string]int64, len(w.Stock))
		for id, qty := range w.Stock {
			if _, exists := e.products[id]; !exists || qty < 0 {
				return nil, fmt.Errorf("invalid stock %s/%s", w.ID, id)
			}
			copyStock[id] = qty
		}
		w.Stock = copyStock
		e.warehouses[i] = w
	}
	sort.Slice(e.warehouses, func(i, j int) bool { return e.warehouses[i].ID < e.warehouses[j].ID })
	return e, nil
}

func (e *Engine) normalize(items []domain.OrderItem) ([]domain.OrderItem, error) {
	if len(items) == 0 {
		return nil, &ValidationError{"items", "at least one order item is required"}
	}
	merged := make(map[string]int64, len(items))
	for i, item := range items {
		field := fmt.Sprintf("items[%d]", i)
		if _, ok := e.products[item.ProductID]; !ok {
			return nil, &ValidationError{field + ".product_id", "unknown product"}
		}
		if item.Quantity <= 0 {
			return nil, &ValidationError{field + ".quantity", "must be a positive integer"}
		}
		if merged[item.ProductID] > math.MaxInt64-item.Quantity {
			return nil, &ValidationError{field + ".quantity", "combined quantity is too large"}
		}
		merged[item.ProductID] += item.Quantity
	}
	out := make([]domain.OrderItem, 0, len(merged))
	for id, qty := range merged {
		out = append(out, domain.OrderItem{ProductID: id, Quantity: qty})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProductID < out[j].ProductID })
	return out, nil
}

func eligible(p domain.Product, w domain.Warehouse) bool {
	return !p.RequiresColdChain || (w.HasColdStorage && w.HasRefrigeratedFleet)
}

func sumStock(warehouses []domain.Warehouse, p domain.Product) (physical, available int64) {
	for _, w := range warehouses {
		qty := w.Stock[p.ID]
		physical += qty
		if eligible(p, w) {
			available += qty
		}
	}
	return physical, available
}

type candidate struct {
	mask       int
	score      int
	distance   int
	count      int
	splitCount int
	allocation []domain.WarehouseAllocation
}

// Recommend evaluates all warehouse subsets; all-or-nothing and cold-chain are hard constraints.
// The score is total one-way distance + penalty*(warehouse count-1), in km-equivalent.
func (e *Engine) Recommend(raw []domain.OrderItem) (domain.Result, error) {
	return e.recommend(raw, nil, nil)
}

// RecommendScheduled adds historical evidence only when the caller supplies a schedule.
// The core balanced score always takes precedence; time can break an exact score tie.
func (e *Engine) RecommendScheduled(raw []domain.OrderItem, schedule *insights.Schedule, history *insights.History) (domain.Result, error) {
	if schedule == nil {
		return e.Recommend(raw)
	}
	if history == nil {
		return domain.Result{}, fmt.Errorf("history is required when a schedule is supplied")
	}
	if _, err := schedule.Period(); err != nil {
		return domain.Result{}, &ValidationError{Field: "delivery_schedule", Message: err.Error()}
	}
	return e.recommend(raw, schedule, history)
}

func (e *Engine) recommend(raw []domain.OrderItem, schedule *insights.Schedule, history *insights.History) (domain.Result, error) {
	items, err := e.normalize(raw)
	if err != nil {
		return domain.Result{}, err
	}
	shortages := make([]domain.Shortage, 0)
	for _, item := range items {
		p := e.products[item.ProductID]
		physical, available := sumStock(e.warehouses, p)
		if available < item.Quantity {
			shortages = append(shortages, domain.Shortage{
				ProductID: item.ProductID, RequestedQuantity: item.Quantity,
				EligibleStock: available, PhysicalStock: physical,
				ShortageQuantity: item.Quantity - available,
			})
		}
	}
	if len(shortages) > 0 {
		return domain.Result{Status: "INSUFFICIENT_ELIGIBLE_STOCK", Recommendation: nil,
			Shortages:   shortages,
			Explanation: "At least one item exceeds available eligible stock. No partial allocation is recommended."}, nil
	}

	var best *candidate
	var ties []*candidate
	for mask := 1; mask < 1<<len(e.warehouses); mask++ {
		selected := e.selected(mask)
		if !e.feasible(selected, items) {
			continue
		}
		distance := 0
		for _, w := range selected {
			distance += w.DistanceKM
		}
		count := bits.OnesCount(uint(mask))
		score := distance + e.penalty*(count-1)
		// Avoid calculating allocations for candidates that cannot win on primary/secondary keys.
		if best != nil && (score > best.score || (schedule == nil && score == best.score && count > best.count)) {
			continue
		}
		alloc, splits, err := e.allocate(selected, items)
		if err != nil {
			return domain.Result{}, fmt.Errorf("allocation invariant failed: %w", err)
		}
		c := &candidate{mask: mask, score: score, distance: distance, count: count, splitCount: splits, allocation: alloc}
		if best == nil || score < best.score {
			best = c
			if schedule != nil {
				ties = []*candidate{c}
			}
		} else if score == best.score {
			if schedule != nil {
				ties = append(ties, c)
			}
			if e.better(c, best) {
				best = c
			}
		}
	}
	// If global eligible stock is sufficient, the full eligible set must be feasible.
	if best == nil {
		return domain.Result{}, fmt.Errorf("no feasible candidate despite sufficient eligible stock")
	}
	// The normal decision remains the fallback whenever historical evidence is sparse.
	coreBest := best
	var historyContext *domain.HistoricalContext
	if schedule != nil {
		period, _ := schedule.Period() // already validated by RecommendScheduled
		coreContext, ready := e.historicalContext(coreBest, *schedule, period, history)
		compared := false
		if ready {
			bestETA := coreContext.IndicativeOrderMinutes
			for _, alternative := range ties {
				if alternative == coreBest {
					continue
				}
				context, sufficient := e.historicalContext(alternative, *schedule, period, history)
				if !sufficient {
					continue
				}
				compared = true
				if context.IndicativeOrderMinutes < bestETA ||
					(context.IndicativeOrderMinutes == bestETA && e.better(alternative, best)) {
					best, bestETA = alternative, context.IndicativeOrderMinutes
				}
			}
		}
		historyContext, _ = e.historicalContext(best, *schedule, period, history)
		historyContext.TieBreakerCompared = compared
		historyContext.ChangedCoreSelection = best != coreBest
		if !ready {
			historyContext.Note = "Historical data for the original selection is sparse. Period averages are advisory only; balanced-score tie-breaking is unchanged."
		} else if !compared {
			historyContext.Note = "Insufficient comparable alternatives at the same balanced score. Historical observations are advisory only."
		} else if best != coreBest {
			historyContext.Note = "Period-matched historical means broke an exact balanced-score tie. All warehouses in both compared combinations have at least two matching records."
		} else {
			historyContext.Note = "Period-matched historical means were considered among equal-score options; the original selection remains preferred."
		}
	}
	rec := &domain.Recommendation{
		Warehouses: best.allocation, WarehouseCount: best.count,
		TotalDistanceKM: best.distance, PenaltyKMEq: e.penalty * (best.count - 1),
		ScoreKMEq: best.score, ProductSplitCount: best.splitCount,
		Explanation: explanation(best, items), History: historyContext,
	}
	if historyContext != nil && historyContext.ChangedCoreSelection {
		rec.Explanation += " With an optional departure schedule, sufficient period-matched history favored this combination over another with the same balanced score."
	}
	if err := e.verify(rec, items); err != nil {
		return domain.Result{}, fmt.Errorf("recommendation invariant failed: %w", err)
	}
	return domain.Result{Status: "FULFILLABLE", Recommendation: rec}, nil
}

// historicalContext estimates the order's completion from the slowest parallel
// warehouse dispatch. A fallback all-time mean can be displayed, but not ranked.
func (e *Engine) historicalContext(c *candidate, schedule insights.Schedule, period string, history *insights.History) (*domain.HistoricalContext, bool) {
	context := &domain.HistoricalContext{Day: schedule.Day, Time: schedule.Time, Period: period,
		Warehouses: make([]domain.HistoricalWarehouse, 0, c.count)}
	sufficientlyObserved := true
	for _, allocation := range c.allocation {
		estimate, exists := history.Estimate(allocation.WarehouseID, period)
		if !exists {
			sufficientlyObserved = false
			continue
		}
		if !estimate.ReliableForTie {
			sufficientlyObserved = false
		}
		context.Warehouses = append(context.Warehouses, domain.HistoricalWarehouse{
			WarehouseID: estimate.WarehouseID, AverageMinutes: estimate.AverageMinutes,
			SampleSize: estimate.SampleSize, Basis: estimate.Basis, ReliableForTie: estimate.ReliableForTie,
		})
		if estimate.AverageMinutes > context.IndicativeOrderMinutes {
			context.IndicativeOrderMinutes = estimate.AverageMinutes
		}
	}
	if len(context.Warehouses) != c.count {
		sufficientlyObserved = false
	}
	return context, sufficientlyObserved
}

func explanation(c *candidate, items []domain.OrderItem) string {
	ids := make([]string, 0, len(c.allocation))
	for _, a := range c.allocation {
		ids = append(ids, a.WarehouseID)
	}
	return fmt.Sprintf("%s fulfill all %d requested product(s) with the lowest balanced score (%d km-equivalent), accounting for one-way distances and extra warehouse dispatches. All product-specific cold-chain requirements are satisfied.", strings.Join(ids, " + "), len(items), c.score)
}

func (e *Engine) selected(mask int) []domain.Warehouse {
	selected := make([]domain.Warehouse, 0, bits.OnesCount(uint(mask)))
	for i, w := range e.warehouses {
		if mask&(1<<i) != 0 {
			selected = append(selected, w)
		}
	}
	return selected
}

func (e *Engine) feasible(selected []domain.Warehouse, items []domain.OrderItem) bool {
	for _, item := range items {
		p := e.products[item.ProductID]
		remaining := item.Quantity
		for _, w := range selected {
			if eligible(p, w) {
				stock := w.Stock[p.ID]
				if stock >= remaining {
					remaining = 0
					break
				}
				remaining -= stock // subtracting prevents overflow for large quantities
			}
		}
		if remaining > 0 {
			return false
		}
	}
	return true
}

func (e *Engine) better(a, b *candidate) bool {
	if a.score != b.score {
		return a.score < b.score
	}
	if a.count != b.count {
		return a.count < b.count
	}
	if a.splitCount != b.splitCount {
		return a.splitCount < b.splitCount
	}
	// Masks compare lexicographically because warehouses have canonical ID order.
	ai, bi := e.selected(a.mask), e.selected(b.mask)
	for i := range ai {
		if ai[i].ID != bi[i].ID {
			return ai[i].ID < bi[i].ID
		}
	}
	return false
}

// allocate minimizes (1) per-product source count, then (2) sum of source distances,
// then (3) source IDs. Chosen source quantities are filled nearest-first.
func (e *Engine) allocate(selected []domain.Warehouse, items []domain.OrderItem) ([]domain.WarehouseAllocation, int, error) {
	allocation := make(map[string]*domain.WarehouseAllocation, len(selected))
	splitCount := 0
	for _, item := range items {
		p := e.products[item.ProductID]
		available := make([]domain.Warehouse, 0, len(selected))
		for _, w := range selected {
			if eligible(p, w) && w.Stock[p.ID] > 0 {
				available = append(available, w)
			}
		}
		var bestSources []domain.Warehouse
		bestDist := math.MaxInt
		for mask := 1; mask < 1<<len(available); mask++ {
			sourceCount := bits.OnesCount(uint(mask))
			if bestSources != nil && sourceCount > len(bestSources) {
				continue
			}
			remaining, distance := item.Quantity, 0
			sources := make([]domain.Warehouse, 0, sourceCount)
			for i, w := range available {
				if mask&(1<<i) == 0 {
					continue
				}
				sources = append(sources, w)
				distance += w.DistanceKM
				if w.Stock[p.ID] >= remaining {
					remaining = 0
				} else {
					remaining -= w.Stock[p.ID]
				}
			}
			if remaining > 0 {
				continue
			}
			if bestSources == nil || sourceCount < len(bestSources) || (sourceCount == len(bestSources) && (distance < bestDist || (distance == bestDist && idsLess(sources, bestSources)))) {
				bestSources, bestDist = sources, distance
			}
		}
		if bestSources == nil {
			return nil, 0, fmt.Errorf("no eligible source set for %s", p.ID)
		}
		splitCount += len(bestSources) - 1
		sort.Slice(bestSources, func(i, j int) bool {
			if bestSources[i].DistanceKM != bestSources[j].DistanceKM {
				return bestSources[i].DistanceKM < bestSources[j].DistanceKM
			}
			return bestSources[i].ID < bestSources[j].ID
		})
		remaining := item.Quantity
		for _, w := range bestSources {
			qty := w.Stock[p.ID]
			if qty > remaining {
				qty = remaining
			}
			if qty <= 0 {
				return nil, 0, fmt.Errorf("empty source in minimal set for %s", p.ID)
			}
			entry := allocation[w.ID]
			if entry == nil {
				entry = &domain.WarehouseAllocation{WarehouseID: w.ID, City: w.City, DistanceKM: w.DistanceKM, Items: []domain.AllocationItem{}}
				allocation[w.ID] = entry
			}
			entry.Items = append(entry.Items, domain.AllocationItem{ProductID: p.ID, Quantity: qty})
			remaining -= qty
		}
		if remaining != 0 {
			return nil, 0, fmt.Errorf("allocation shortfall for %s", p.ID)
		}
	}
	// An unused warehouse in a chosen candidate signals a bug: removing it would lower score.
	if len(allocation) != len(selected) {
		return nil, 0, fmt.Errorf("candidate includes unused warehouses")
	}
	out := make([]domain.WarehouseAllocation, 0, len(allocation))
	for _, w := range selected {
		a := allocation[w.ID]
		sort.Slice(a.Items, func(i, j int) bool { return a.Items[i].ProductID < a.Items[j].ProductID })
		out = append(out, *a)
	}
	return out, splitCount, nil
}

func idsLess(a, b []domain.Warehouse) bool {
	for i := range a {
		if a[i].ID != b[i].ID {
			return a[i].ID < b[i].ID
		}
	}
	return false
}

func (e *Engine) verify(r *domain.Recommendation, items []domain.OrderItem) error {
	if len(r.Warehouses) != r.WarehouseCount {
		return fmt.Errorf("incorrect warehouse count")
	}
	totals := make(map[string]int64, len(items))
	seen := make(map[string]bool)
	for _, a := range r.Warehouses {
		if seen[a.WarehouseID] || len(a.Items) == 0 {
			return fmt.Errorf("duplicate or empty warehouse")
		}
		seen[a.WarehouseID] = true
		var warehouse *domain.Warehouse
		for i := range e.warehouses {
			if e.warehouses[i].ID == a.WarehouseID {
				warehouse = &e.warehouses[i]
				break
			}
		}
		if warehouse == nil {
			return fmt.Errorf("unknown allocation warehouse")
		}
		for _, item := range a.Items {
			p, ok := e.products[item.ProductID]
			if !ok || item.Quantity <= 0 || !eligible(p, *warehouse) || item.Quantity > warehouse.Stock[item.ProductID] {
				return fmt.Errorf("invalid allocation at %s for %s", a.WarehouseID, item.ProductID)
			}
			totals[item.ProductID] += item.Quantity
		}
	}
	for _, item := range items {
		if totals[item.ProductID] != item.Quantity {
			return fmt.Errorf("quantity mismatch for %s", item.ProductID)
		}
	}
	return nil
}
