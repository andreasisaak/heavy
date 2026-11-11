package sampler

import "time"

// window is how long a series keeps its values.
const window = 30 * time.Minute

type point struct {
	time  time.Time
	value float64
}

// Series holds the values of one measure of the last 30 minutes, oldest first.
type Series struct {
	points []point
}

// Add appends a value and drops those more than 30 minutes older than it.
func (s *Series) Add(at time.Time, value float64) {
	s.points = append(s.points, point{time: at, value: value})
	drop := 0
	for drop < len(s.points) && at.Sub(s.points[drop].time) > window {
		drop++
	}
	s.points = s.points[drop:]
}

// Mean is the average of the values taken within the given time before the newest one; 0 without values.
func (s *Series) Mean(over time.Duration) float64 {
	if len(s.points) == 0 {
		return 0
	}
	newest := s.points[len(s.points)-1].time
	sum, count := 0.0, 0
	for index := len(s.points) - 1; index >= 0 && newest.Sub(s.points[index].time) < over; index-- {
		sum += s.points[index].value
		count++
	}
	return sum / float64(count)
}

// HeldFor is how long the condition has held without a break, from the first value of the newest unbroken stretch
// to the newest value; 0 when the newest value does not meet it.
func (s *Series) HeldFor(condition func(float64) bool) time.Duration {
	if len(s.points) == 0 || !condition(s.points[len(s.points)-1].value) {
		return 0
	}
	first := len(s.points) - 1
	for first > 0 && condition(s.points[first-1].value) {
		first--
	}
	return s.points[len(s.points)-1].time.Sub(s.points[first].time)
}

// Lows are the lowest values of the last count steps of the given width before now, oldest first; a step without
// values is left out, so a series younger than count steps gives fewer.
func (s *Series) Lows(now time.Time, width time.Duration, count int) []float64 {
	var lows []float64
	for step := count; step > 0; step-- {
		from, to := now.Add(-time.Duration(step)*width), now.Add(-time.Duration(step-1)*width)
		found := false
		var low float64
		for _, current := range s.points {
			if current.time.After(from) && !current.time.After(to) && (!found || current.value < low) {
				low, found = current.value, true
			}
		}
		if found {
			lows = append(lows, low)
		}
	}
	return lows
}
