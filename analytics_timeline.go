package main

import (
	"fmt"
	"strings"
	"time"
)

type timelineDot struct {
	X, Y  float64
	Value uint64
	Label string
}
type timelineSeries struct {
	Label, Color, Points string
	Dots                 []timelineDot
}
type timelineTick struct {
	Y     float64
	Value uint64
}
type timelineLabel struct {
	X    float64
	Text string
}
type analyticsTimeline struct {
	Range, Since, Resolution string
	Series                   []timelineSeries
	Ticks                    []timelineTick
	Labels                   []timelineLabel
	HasEvents                bool
}

func buildAnalyticsTimeline(snapshot AnalyticsSnapshot, key string, now time.Time) analyticsTimeline {
	step := 24 * time.Hour
	count := 7
	switch key {
	case "24h":
		step = time.Hour
		count = 24
	case "30d":
		count = 30
	case "90d":
		count = 90
	default:
		key = "7d"
	}
	result := analyticsTimeline{Range: key, Resolution: "Daily counts (UTC)"}
	if step == time.Hour {
		result.Resolution = "Hourly counts (UTC)"
	}
	if snapshot.HistorySince.IsZero() {
		return result
	}
	result.Since = snapshot.HistorySince.UTC().Format("Jan 2, 2006, 15:04 UTC")
	end := now.UTC().Truncate(step)
	start := end.Add(-time.Duration(count-1) * step)
	first := snapshot.HistorySince.UTC().Truncate(step)
	if start.Before(first) {
		start = first
	}
	if start.After(end) {
		return result
	}
	buckets := make(map[time.Time]SoundCounts)
	for _, bucket := range snapshot.Hourly {
		at := bucket.At.UTC().Truncate(step)
		if at.Before(start) || at.After(end) {
			continue
		}
		counts := buckets[at]
		counts.Added += bucket.Added
		counts.Sent += bucket.Sent
		counts.Played += bucket.Played
		buckets[at] = counts
	}
	var times []time.Time
	var maximum uint64 = 1
	for at := start; !at.After(end); at = at.Add(step) {
		times = append(times, at)
		counts := buckets[at]
		for _, value := range []uint64{counts.Played, counts.Sent, counts.Added} {
			if value > maximum {
				maximum = value
			}
			if value > 0 {
				result.HasEvents = true
			}
		}
	}
	result.Ticks = []timelineTick{{Y: 210, Value: 0}, {Y: 22, Value: maximum}}
	if maximum > 1 {
		middle := maximum / 2
		result.Ticks = append(result.Ticks, timelineTick{Y: 210 - float64(middle)/float64(maximum)*188, Value: middle})
	}
	for _, spec := range []struct {
		label, color string
		value        func(SoundCounts) uint64
	}{
		{"Plays", "#5865f2", func(c SoundCounts) uint64 { return c.Played }},
		{"Sends", "#0891b2", func(c SoundCounts) uint64 { return c.Sent }},
		{"Additions", "#a855f7", func(c SoundCounts) uint64 { return c.Added }},
	} {
		series := timelineSeries{Label: spec.label, Color: spec.color}
		var points []string
		for index, at := range times {
			x := 415.0
			if len(times) > 1 {
				x = 52 + float64(index)/float64(len(times)-1)*726
			}
			value := spec.value(buckets[at])
			y := 210 - float64(value)/float64(maximum)*188
			label := at.Format("Jan 2, 2006")
			if step == time.Hour {
				label = at.Format("Jan 2, 2006 15:00 UTC")
			}
			series.Dots = append(series.Dots, timelineDot{X: x, Y: y, Value: value, Label: label})
			points = append(points, fmt.Sprintf("%.2f,%.2f", x, y))
		}
		series.Points = strings.Join(points, " ")
		result.Series = append(result.Series, series)
	}
	seen := map[int]bool{}
	for _, index := range []int{0, len(times) / 2, len(times) - 1} {
		if seen[index] {
			continue
		}
		seen[index] = true
		label := times[index].Format("Jan 2")
		if step == time.Hour {
			label = times[index].Format("Jan 2 15:00")
		}
		x := 415.0
		if len(times) > 1 {
			x = 52 + float64(index)/float64(len(times)-1)*726
		}
		result.Labels = append(result.Labels, timelineLabel{X: x, Text: label})
	}
	return result
}
