package scanner

import (
	"regexp"
	"strings"
	"time"

	"kubectl-checkpods/internal/config"
	"kubectl-checkpods/pkg/types"
)

// MatchResult describes the result of matching a log line.
type MatchResult struct {
	Matched  bool
	Keyword  string
	Excluded bool
}

// Matcher performs log line pattern matching.
type Matcher struct {
	keywords      []string
	excludes      []string
	regexPatterns []*regexp.Regexp
}

// NewMatcher creates a new Matcher from the given config.
func NewMatcher(cfg *config.Config) *Matcher {
	return &Matcher{
		keywords:      cfg.Keywords,
		excludes:      cfg.Excludes,
		regexPatterns: cfg.RegexPatterns,
	}
}

// Match checks if a log line matches any keyword or regex pattern.
// Returns the match result with the matched keyword and whether it was excluded.
func (m *Matcher) Match(line string) MatchResult {
	lowerLine := strings.ToLower(line)

	// Check exclusions first
	for _, ex := range m.excludes {
		if strings.Contains(lowerLine, ex) {
			return MatchResult{Excluded: true}
		}
	}

	// Check regex patterns
	for _, re := range m.regexPatterns {
		if re.MatchString(line) {
			return MatchResult{
				Matched: true,
				Keyword: re.String(),
			}
		}
	}

	// Check substring keywords
	for _, kw := range m.keywords {
		if strings.Contains(lowerLine, kw) {
			return MatchResult{
				Matched: true,
				Keyword: kw,
			}
		}
	}

	return MatchResult{}
}

// DedupWindow tracks matching lines within a time window to avoid duplicates.
type DedupWindow struct {
	window    time.Duration
	lastMatch map[string]time.Time
}

// NewDedupWindow creates a new deduplication window.
func NewDedupWindow(window time.Duration) *DedupWindow {
	return &DedupWindow{
		window:    window,
		lastMatch: make(map[string]time.Time),
	}
}

// ShouldReport checks if a match should be reported based on dedup rules.
// The key is typically "namespace/pod/container/keyword".
func (d *DedupWindow) ShouldReport(key string) bool {
	now := time.Now()
	if last, ok := d.lastMatch[key]; ok {
		if now.Sub(last) < d.window {
			return false
		}
	}
	d.lastMatch[key] = now
	return true
}

// BuildLogMatch constructs a LogMatch from raw scanning data.
func BuildLogMatch(ns, pod, container, deployment, keyword, line string, lineNum int64) types.LogMatch {
	return types.LogMatch{
		Timestamp:  time.Now(),
		Namespace:  ns,
		PodName:    pod,
		Container:  container,
		Deployment: deployment,
		Keyword:    keyword,
		Line:       line,
		LineNumber: lineNum,
	}
}
