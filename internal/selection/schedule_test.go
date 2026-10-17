package selection

import (
	"example.com/simpul-warehouse-selection/internal/assets"
	"example.com/simpul-warehouse-selection/internal/domain"
	"example.com/simpul-warehouse-selection/internal/insights"
	"reflect"
	"strings"
	"testing"
)

func csvHistory(t *testing.T) *insights.History {
	t.Helper()
	raw, err := assets.HistoryCSV()
	if err != nil {
		t.Fatal(err)
	}
	h, err := insights.ParseCSV(raw)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func warehouseIDs(r domain.Result) []string {
	var ids []string
	if r.Recommendation == nil {
		return nil
	}
	for _, w := range r.Recommendation.Warehouses {
		ids = append(ids, w.WarehouseID)
	}
	return ids
}

func TestScheduledRecommendationKeepsColdChainAndCoreScore(t *testing.T) {
	e := testEngine(t)
	history := csvHistory(t)
	raw := order("P002", 45)
	core, err := e.Recommend(raw)
	if err != nil {
		t.Fatal(err)
	}
	schedule := &insights.Schedule{Day: "Wed", Time: "08:00"}
	scheduled, err := e.RecommendScheduled(raw, schedule, history)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(warehouseIDs(core), warehouseIDs(scheduled)) || core.Recommendation.ScoreKMEq != scheduled.Recommendation.ScoreKMEq {
		t.Fatalf("score regression: %+v", scheduled)
	}
	ctx := scheduled.Recommendation.History
	if ctx == nil || ctx.Period != insights.WeekdayPeak || ctx.IndicativeOrderMinutes < 1 || len(ctx.Warehouses) != 2 {
		t.Fatalf("bad historical context: %+v", ctx)
	}
	if ctx.TieBreakerCompared {
		t.Fatal("there is no comparable equal-score alternative for this order")
	}
	old, err := e.RecommendScheduled(raw, nil, history)
	if err != nil || !reflect.DeepEqual(old, core) {
		t.Fatal("omitting schedule must reproduce the original result")
	}
	shortage, err := e.RecommendScheduled(order("P002", 109), schedule, history)
	if err != nil || shortage.Status != "INSUFFICIENT_ELIGIBLE_STOCK" || shortage.Recommendation != nil {
		t.Fatalf("all-or-nothing regression %+v %v", shortage, err)
	}
}

func TestScheduledTieMayPreferThreeWarehouses(t *testing.T) {
	// A 2-source and 3-source solution have identical balanced scores of 115.
	c := domain.Catalog{Products: []domain.Product{{ID: "P001"}}, Warehouses: []domain.Warehouse{
		{ID: "W01", DistanceKM: 5, Stock: map[string]int64{"P001": 200}},
		{ID: "W02", DistanceKM: 25, Stock: map[string]int64{"P001": 100}},
		{ID: "W03", DistanceKM: 45, Stock: map[string]int64{"P001": 300}},
		{ID: "W04", DistanceKM: 90, Stock: map[string]int64{"P001": 400}},
	}}
	e, err := New(c, 20)
	if err != nil {
		t.Fatal(err)
	}
	raw := order("P001", 600)
	baseline, err := e.Recommend(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := warehouseIDs(baseline); !reflect.DeepEqual(got, []string{"W01", "W04"}) {
		t.Fatalf("unexpected core %v", got)
	}
	csv := "Delivery ID,Warehouse,Day/Time,Actual Duration (minutes)\n"
	for _, entry := range []struct {
		id   string
		a, b int
	}{{"W01", 30, 35}, {"W02", 36, 38}, {"W03", 45, 50}, {"W04", 120, 130}} {
		csv += "1," + entry.id + ",\"Mon, 08:00\"," + itoa(entry.a) + "\n"
		csv += "2," + entry.id + ",\"Wed, 17:00\"," + itoa(entry.b) + "\n"
	}
	history, err := insights.ParseCSV([]byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	schedule := &insights.Schedule{Day: "Tue", Time: "08:30"}
	res, err := e.RecommendScheduled(raw, schedule, history)
	if err != nil {
		t.Fatal(err)
	}
	if ids := warehouseIDs(res); !reflect.DeepEqual(ids, []string{"W01", "W02", "W03"}) {
		t.Fatalf("historical tie-break did not favor three sources: %v", ids)
	}
	if res.Recommendation.ScoreKMEq != 115 || !res.Recommendation.History.ChangedCoreSelection || !res.Recommendation.History.TieBreakerCompared {
		t.Fatalf("bad result %+v", res.Recommendation)
	}
	if res.Recommendation.History.IndicativeOrderMinutes != 47.5 {
		t.Fatalf("expected max parallel = 47.5, got %f", res.Recommendation.History.IndicativeOrderMinutes)
	}
	// Same order, but other period has no period-specific samples: original ranking wins.
	weekend := &insights.Schedule{Day: "Sun", Time: "08:30"}
	res, err = e.RecommendScheduled(raw, weekend, history)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(warehouseIDs(res), warehouseIDs(baseline)) || res.Recommendation.History.TieBreakerCompared {
		t.Fatalf("sparse-history fallback failed: %+v", res.Recommendation)
	}
}

func TestScheduleValidation(t *testing.T) {
	e := testEngine(t)
	for _, sched := range []insights.Schedule{{Day: "Foo", Time: "08:00"}, {Day: "Mon", Time: "25:00"}} {
		_, err := e.RecommendScheduled(order("P001", 5), &sched, csvHistory(t))
		if err == nil || !strings.Contains(err.Error(), "delivery_schedule") {
			t.Fatalf("expected validation error, got %v", err)
		}
	}
}
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	out := ""
	for v > 0 {
		out = string(rune('0'+v%10)) + out
		v /= 10
	}
	return out
}
