// Package insights analyzes the small historical delivery sample.
// Period means are descriptive observations, NOT guaranteed future delivery times.
package insights

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// MinimumSamples prevents tiny period samples from deciding between tied routes.
const MinimumSamples = 2

const (
	WeekdayPeak    = "weekday_peak"
	WeekdayOffpeak = "weekday_offpeak"
	Weekend        = "weekend"
)

type Schedule struct {
	Day  string `json:"day"`
	Time string `json:"time"`
}

// Period groups by local departure day and time. No live traffic or calendar is used.
func (s Schedule) Period() (string, error) {
	switch s.Day {
	case "Sat", "Sun":
		// The time must still be valid, even though weekend data is pooled.
		if _, err := minutesOfDay(s.Time); err != nil {
			return "", err
		}
		return Weekend, nil
	case "Mon", "Tue", "Wed", "Thu", "Fri":
		minute, err := minutesOfDay(s.Time)
		if err != nil {
			return "", err
		}
		if (minute >= 7*60 && minute < 10*60) || (minute >= 16*60 && minute < 19*60) {
			return WeekdayPeak, nil
		}
		return WeekdayOffpeak, nil
	default:
		return "", fmt.Errorf("day must be Mon, Tue, Wed, Thu, Fri, Sat, or Sun")
	}
}

func minutesOfDay(value string) (int, error) {
	if len(value) != 5 || value[2] != ':' {
		return 0, fmt.Errorf("time must be HH:MM (24-hour local time)")
	}
	for _, i := range []int{0, 1, 3, 4} {
		if value[i] < '0' || value[i] > '9' {
			return 0, fmt.Errorf("time must be HH:MM (24-hour local time)")
		}
	}
	h := int(value[0]-'0')*10 + int(value[1]-'0')
	m := int(value[3]-'0')*10 + int(value[4]-'0')
	if h > 23 || m > 59 {
		return 0, fmt.Errorf("time must be HH:MM (24-hour local time)")
	}
	return h*60 + m, nil
}

type PeriodMetric struct {
	Period         string  `json:"period"`
	Deliveries     int     `json:"deliveries"`
	AverageMinutes float64 `json:"average_minutes"`
}

type WarehouseMetric struct {
	WarehouseID    string         `json:"warehouse_id"`
	Deliveries     int            `json:"deliveries"`
	AverageMinutes float64        `json:"average_minutes"`
	Periods        []PeriodMetric `json:"periods"`
}

type Report struct {
	TotalRecords int               `json:"total_records"`
	Warehouses   []WarehouseMetric `json:"warehouses"`
	Method       string            `json:"method"`
	Caveat       string            `json:"caveat"`
}

type durationStats struct{ count, total int }

func (a durationStats) mean() float64 { return float64(a.total) / float64(a.count) }
func rounded(value float64) float64   { return math.Round(value*10) / 10 }

// History is populated once at startup, then accessed read-only by concurrent requests.
type History struct {
	all     map[string]durationStats
	periods map[string]map[string]durationStats
	records int
}

func ParseCSV(data []byte) (*History, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read history CSV: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("delivery history is empty")
	}
	header := records[0]
	if len(header) < 4 || strings.TrimSpace(header[1]) != "Warehouse" || strings.TrimSpace(header[2]) != "Day/Time" || strings.TrimSpace(header[3]) != "Actual Duration (minutes)" {
		return nil, fmt.Errorf("unexpected historical CSV columns")
	}
	history := &History{all: map[string]durationStats{}, periods: map[string]map[string]durationStats{}}
	for i, row := range records[1:] {
		if len(row) < 4 {
			return nil, fmt.Errorf("history line %d missing fields", i+2)
		}
		id := strings.TrimSpace(row[1])
		parts := strings.Split(strings.TrimSpace(row[2]), ",")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid history day/time on line %d", i+2)
		}
		period, periodErr := (Schedule{Day: strings.TrimSpace(parts[0]), Time: strings.TrimSpace(parts[1])}).Period()
		minutes, durationErr := strconv.Atoi(strings.TrimSpace(row[3]))
		if id == "" || periodErr != nil || durationErr != nil || minutes <= 0 {
			return nil, fmt.Errorf("invalid delivery record on line %d", i+2)
		}
		a := history.all[id]
		a.count++
		a.total += minutes
		history.all[id] = a
		if history.periods[id] == nil {
			history.periods[id] = map[string]durationStats{}
		}
		p := history.periods[id][period]
		p.count++
		p.total += minutes
		history.periods[id][period] = p
		history.records++
	}
	return history, nil
}

func AnalyzeCSV(data []byte) (Report, error) {
	h, err := ParseCSV(data)
	if err != nil {
		return Report{}, err
	}
	return h.Report(), nil
}

func (h *History) Report() Report {
	out := Report{TotalRecords: h.records, Warehouses: make([]WarehouseMetric, 0, len(h.all)),
		Method: "Arithmetic means by warehouse and departure period. Peak = Mon-Fri 07:00-09:59 or 16:00-18:59; off-peak = other weekday hours; weekend = Sat-Sun. Times are local to the sample.",
		Caveat: "38 observations; uneven groups, no route/traffic controls. Sample means are indicative, not a reliable ETA. Only ties in the core score can use period means, and only with at least two matching observations for EVERY candidate warehouse.",
	}
	for id, a := range h.all {
		metric := WarehouseMetric{WarehouseID: id, Deliveries: a.count, AverageMinutes: rounded(a.mean()), Periods: []PeriodMetric{}}
		for _, period := range []string{WeekdayPeak, WeekdayOffpeak, Weekend} {
			p := h.periods[id][period]
			if p.count > 0 {
				metric.Periods = append(metric.Periods, PeriodMetric{Period: period, Deliveries: p.count, AverageMinutes: rounded(p.mean())})
			}
		}
		out.Warehouses = append(out.Warehouses, metric)
	}
	sort.Slice(out.Warehouses, func(i, j int) bool { return out.Warehouses[i].WarehouseID < out.Warehouses[j].WarehouseID })
	return out
}

type WarehouseDuration struct {
	WarehouseID    string  `json:"warehouse_id"`
	AverageMinutes float64 `json:"average_minutes"`
	SampleSize     int     `json:"sample_size"`
	Basis          string  `json:"basis"` // matched_period or all_history_fallback
	ReliableForTie bool    `json:"reliable_for_tie"`
}

// Estimate uses the selected period only if n>=2, falling back to all history
// for display only. Fallback observations NEVER decide a tie.
func (h *History) Estimate(id, period string) (WarehouseDuration, bool) {
	selected := h.periods[id][period]
	if selected.count >= MinimumSamples {
		return WarehouseDuration{WarehouseID: id, AverageMinutes: rounded(selected.mean()), SampleSize: selected.count, Basis: "matched_period", ReliableForTie: true}, true
	}
	all := h.all[id]
	if all.count == 0 {
		return WarehouseDuration{}, false
	}
	return WarehouseDuration{WarehouseID: id, AverageMinutes: rounded(all.mean()), SampleSize: all.count, Basis: "all_history_fallback", ReliableForTie: false}, true
}
