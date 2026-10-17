package insights

import (
	"example.com/simpul-warehouse-selection/internal/assets"
	"testing"
)

func TestAnalyzeCSV(t *testing.T) {
	data, err := assets.HistoryCSV()
	if err != nil {
		t.Fatal(err)
	}
	report, err := AnalyzeCSV(data)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalRecords != 38 || len(report.Warehouses) != 8 {
		t.Fatalf("unexpected report %+v", report)
	}
	want := []float64{34.1, 54.6, 51, 57.6, 156, 163.8, 67.5, 207.5}
	for i, m := range report.Warehouses {
		if m.AverageMinutes != want[i] {
			t.Fatalf("%s: %.1f, expected %.1f", m.WarehouseID, m.AverageMinutes, want[i])
		}
	}
}
