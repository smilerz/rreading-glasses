package internal

import (
	"fmt"
	"regexp"
	"sync/atomic"
)

// Hardcover files translations published in several volumes as separate
// works inside series like "The Stand (Split Volumes)". They clutter an
// author's list, so works in a series whose title matches the configured
// pattern can be left out.

var ignoreSeries atomic.Pointer[regexp.Regexp]

// SetIgnoreSeries configures the case-insensitive pattern for series titles
// whose works are left out of author lists. An empty pattern turns it off.
func SetIgnoreSeries(pattern string) error {
	if pattern == "" {
		ignoreSeries.Store(nil)
		return nil
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return fmt.Errorf("invalid series pattern: %w", err)
	}
	ignoreSeries.Store(re)
	return nil
}

// inIgnoredSeries reports whether the work belongs to a series that is ignored.
func inIgnoredSeries(w workResource) bool {
	re := ignoreSeries.Load()
	if re == nil {
		return false
	}
	for _, s := range w.Series {
		if re.MatchString(s.Title) {
			return true
		}
	}
	return false
}
