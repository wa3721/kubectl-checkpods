package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Config holds all configuration for the monitor.
type Config struct {
	// Kubeconfig path
	Kubeconfig string

	// Namespace to monitor (empty = all)
	Namespace string

	// Label selector for pod/deployment filtering
	Selector string

	// How long to wait for a pod to become ready
	ReadyTimeout time.Duration

	// How long to scan pod logs after ready
	LogDuration time.Duration

	// Number of log lines to tail
	TailLines int64

	// Patterns to match in logs (case-insensitive substrings)
	Keywords []string

	// Regex patterns to match in logs
	RegexPatterns []*regexp.Regexp

	// Patterns to exclude (case-insensitive substrings that override match)
	Excludes []string

	// Whether to output ANSI colors
	NoColor bool

	// Whether to output JSON instead of human-readable text
	JSONOutput bool

	// Maximum number of concurrent pod processors
	MaxWorkers int

	// Whether to only watch pods that are part of a Deployment
	DeploymentsOnly bool

	// Per-deployment timeout: if set, the monitor exits once all deployments
	// being watched have completed their rollout and log scanning
	ExitOnComplete bool
}

// Validate checks the configuration for logical errors.
func (c *Config) Validate() error {
	if c.ReadyTimeout <= 0 {
		return fmt.Errorf("ready-timeout must be positive")
	}
	if c.LogDuration <= 0 {
		return fmt.Errorf("log-duration must be positive")
	}
	if c.TailLines < 0 {
		return fmt.Errorf("tail must be non-negative")
	}
	if c.MaxWorkers < 1 {
		c.MaxWorkers = 10
	}
	return nil
}

// DefaultKeywords returns the default set of log keywords.
func DefaultKeywords() []string {
	return []string{"error", "fatal"}
}

// ApplyDefaults fills in default values for unset fields.
func (c *Config) ApplyDefaults() {
	if c.ReadyTimeout == 0 {
		c.ReadyTimeout = 3 * time.Minute
	}
	if c.LogDuration == 0 {
		c.LogDuration = 2 * time.Minute
	}
	if c.TailLines == 0 {
		c.TailLines = 100
	}
	if c.MaxWorkers == 0 {
		c.MaxWorkers = 10
	}
	if len(c.Keywords) == 0 {
		c.Keywords = DefaultKeywords()
	}
	// Lowercase all keywords for case-insensitive matching
	for i, kw := range c.Keywords {
		c.Keywords[i] = strings.ToLower(strings.TrimSpace(kw))
	}
	// Lowercase all excludes
	for i, ex := range c.Excludes {
		c.Excludes[i] = strings.ToLower(strings.TrimSpace(ex))
	}
}
