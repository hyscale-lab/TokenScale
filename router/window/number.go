package window

import "time"

// helper functions for number related windows
type Number interface {
	~int | ~int64 | ~float32 | ~float64
}
// Max returns the largest Need in the current window.
// If the window is empty it returns 0 and false.
func Max[T Number] (w *Window[T], now time.Time) (T, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trimStaleLocked(now)

	if len(w.buffer) == 0 {
		return 0, false
	}
	max := w.buffer[0].Value
	for _, s := range w.buffer[1:] {
		if s.Value > max {
			max = s.Value
		}
	}
	return max, true

}
// Avg returns the average Need value in the current window.
// If the window is empty, it returns 0 and false.
func Avg[T Number] (w *Window[T], now time.Time) (float64, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.trimStaleLocked(now)

	if len(w.buffer) == 0 {
		return 0, false
	}

	var sum T
	for _, s := range w.buffer {
		sum += s.Value
	}
	return float64(sum) / float64(len(w.buffer)), true
}