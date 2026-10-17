package domain

// Catalog is immutable after startup. The service never reserves or deducts stock.
type Catalog struct {
	Products   []Product   `json:"products"`
	Warehouses []Warehouse `json:"warehouses"`
}

type Product struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Category          string `json:"category"`
	RequiresColdChain bool   `json:"requires_cold_chain"`
}

type Warehouse struct {
	ID                   string           `json:"id"`
	City                 string           `json:"city"`
	DistanceKM           int              `json:"distance_km"`
	HasColdStorage       bool             `json:"has_cold_storage"`
	HasRefrigeratedFleet bool             `json:"has_refrigerated_fleet"`
	Stock                map[string]int64 `json:"stock"`
}

type OrderItem struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
}

type AllocationItem struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
}

type WarehouseAllocation struct {
	WarehouseID string           `json:"warehouse_id"`
	City        string           `json:"city"`
	DistanceKM  int              `json:"distance_km"`
	Items       []AllocationItem `json:"items"`
}

// HistoricalContext is optional advisory evidence, never a guarantee of delivery time.
type HistoricalContext struct {
	Day                    string                `json:"day"`
	Time                   string                `json:"time"`
	Period                 string                `json:"period"`
	IndicativeOrderMinutes float64               `json:"indicative_order_minutes"`
	Warehouses             []HistoricalWarehouse `json:"warehouses"`
	TieBreakerCompared     bool                  `json:"tie_breaker_compared"`
	ChangedCoreSelection   bool                  `json:"changed_core_selection"`
	Note                   string                `json:"note"`
}

type HistoricalWarehouse struct {
	WarehouseID    string  `json:"warehouse_id"`
	AverageMinutes float64 `json:"average_minutes"`
	SampleSize     int     `json:"sample_size"`
	Basis          string  `json:"basis"`
	ReliableForTie bool    `json:"reliable_for_tie"`
}

type Recommendation struct {
	Warehouses        []WarehouseAllocation `json:"warehouses"`
	WarehouseCount    int                   `json:"warehouse_count"`
	TotalDistanceKM   int                   `json:"total_distance_km"`
	PenaltyKMEq       int                   `json:"penalty_km_equivalent"`
	ScoreKMEq         int                   `json:"score_km_equivalent"`
	ProductSplitCount int                   `json:"product_split_count"`
	History           *HistoricalContext    `json:"history,omitempty"`
	Explanation       string                `json:"explanation"`
}

type Shortage struct {
	ProductID         string `json:"product_id"`
	RequestedQuantity int64  `json:"requested_quantity"`
	EligibleStock     int64  `json:"eligible_stock"`
	PhysicalStock     int64  `json:"physical_stock"`
	ShortageQuantity  int64  `json:"shortage_quantity"`
}

type Result struct {
	Status         string          `json:"status"`
	Recommendation *Recommendation `json:"recommendation"`
	Shortages      []Shortage      `json:"shortages,omitempty"`
	Explanation    string          `json:"explanation,omitempty"`
}
