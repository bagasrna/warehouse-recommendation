package insights

import (
	"example.com/simpul-warehouse-selection/internal/assets"
	"strings"
	"testing"
)

func TestSchedulePeriods(t *testing.T) {
	cases := []struct {
		day, time, period string
		valid             bool
	}{
		{"Mon", "08:00", WeekdayPeak, true}, {"Fri", "18:59", WeekdayPeak, true},
		{"Fri", "19:00", WeekdayOffpeak, true}, {"Tue", "09:59", WeekdayPeak, true},
		{"Wed", "10:00", WeekdayOffpeak, true}, {"Sat", "07:30", Weekend, true},
		{"Sun", "21:00", Weekend, true}, {"Bad", "08:00", "", false},
		{"Mon", "24:00", "", false}, {"Mon", "9:30", "", false},
		{"Sat", "", "", false}, {"Mon", "08:60", "", false},
	}
	for _, tt := range cases {
		period, err := (Schedule{Day: tt.day, Time: tt.time}).Period()
		if (err == nil) != tt.valid || (tt.valid && period != tt.period) {
			t.Errorf("%s %s: %q, %v", tt.day, tt.time, period, err)
		}
	}
}

func TestHistoryPeriodMetricsAndFallback(t *testing.T) {
	raw, err := assets.HistoryCSV()
	if err != nil {
		t.Fatal(err)
	}
	h, err := ParseCSV(raw)
	if err != nil {
		t.Fatal(err)
	}
	report := h.Report()
	if report.TotalRecords != 38 {
		t.Fatalf("got %d records", report.TotalRecords)
	}
	period, _ := (Schedule{Day: "Mon", Time: "08:00"}).Period()
	peak, exists := h.Estimate("W01", period)
	if !exists || !peak.ReliableForTie || peak.AverageMinutes != 54.5 || peak.SampleSize != 4 || peak.Basis != "matched_period" {
		t.Fatalf("W01 peak %+v", peak)
	}
	off, exists := h.Estimate("W08", WeekdayOffpeak)
	if !exists || off.ReliableForTie || off.Basis != "all_history_fallback" || off.SampleSize != 2 || off.AverageMinutes != 207.5 {
		t.Fatalf("W08 offpeak %+v", off)
	}
	if _, ok := h.Estimate("unknown", period); ok {
		t.Fatal("unknown warehouse must not have history")
	}
}

func TestHistoryRejectsMalformedData(t *testing.T) {
	for _, raw := range []string{
		"Warehouse,Day/Time\nW01,Mon 08:00",
		"Delivery ID,Warehouse,Day/Time,Actual Duration (minutes)\n1,W01,Mon 08:00,20\n",
		"Delivery ID,Warehouse,Day/Time,Actual Duration (minutes)\n1,W01,Mon,08:00,20\n",
		"Delivery ID,Warehouse,Day/Time,Actual Duration (minutes)\n1,W01,Mon,broken\n",
	} {
		if _, err := ParseCSV([]byte(strings.ReplaceAll(raw, "\\n", "\n"))); err == nil {
			t.Errorf("expected failure: %s", raw)
		}
	}
}
