package notifier

import (
	"fmt"
	"sync"
	"time"

	"kubectl-checkpods/pkg/types"
)

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"
)

// ConsoleNotifier prints human-readable output to stdout.
type ConsoleNotifier struct {
	useColor    bool
	mu          sync.Mutex
	summary     *SummaryCollector
}

// SummaryCollector aggregates statistics during monitoring.
type SummaryCollector struct {
	mu         sync.Mutex
	startTime  time.Time
	podOK      int
	podError   int
	podTimeout int
}

// NewConsoleNotifier creates a console notifier.
func NewConsoleNotifier(useColor bool) *ConsoleNotifier {
	return &ConsoleNotifier{
		useColor: useColor,
		summary:  &SummaryCollector{startTime: time.Now()},
	}
}

// Summary returns a copy of the current summary.
func (c *ConsoleNotifier) Summary() types.MonitorResult {
	c.summary.mu.Lock()
	defer c.summary.mu.Unlock()
	return types.MonitorResult{
		StartTime: c.summary.startTime,
		PodsOK:    c.summary.podOK,
		PodsError: c.summary.podError,
		PodsTimeout: c.summary.podTimeout,
		PodsTotal: c.summary.podOK + c.summary.podError + c.summary.podTimeout,
	}
}

func (c *ConsoleNotifier) color(clr, s string) string {
	if !c.useColor {
		return s
	}
	return clr + s + colorReset
}

// OnDeploymentEvent prints a deployment progress line.
func (c *ConsoleNotifier) OnDeploymentEvent(event types.DeploymentEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch event.Phase {
	case types.PhaseInProgress:
		fmt.Printf("%s [DEPLOY] %s/%s rollout started (desired: %d)\n",
			c.color(colorCyan, time.Now().Format(time.RFC3339)),
			event.Namespace, event.Name, event.DesiredReplicas)
	case types.PhaseComplete:
		fmt.Printf("%s [DEPLOY] %s/%s rollout complete (%d/%d ready, %d ok, %d errors)\n",
			c.color(colorGreen, time.Now().Format(time.RFC3339)),
			event.Namespace, event.Name,
			event.ReadyReplicas, event.DesiredReplicas,
			event.PodsOK, event.PodsError)
	}
}

// OnPodEvent prints a pod lifecycle event.
func (c *ConsoleNotifier) OnPodEvent(event types.PodEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch event.Status {
	case types.PodStatusWaiting:
		return
	case types.PodStatusReady:
		fmt.Printf("%s [READY] %s/%s (deploy: %s, took %v)\n",
			c.color(colorGreen, time.Now().Format(time.RFC3339)),
			event.Namespace, event.PodName, event.Deployment, event.ReadyLatency.Round(time.Second))
	case types.PodStatusTimeout:
		c.summary.podTimeout++
		fmt.Printf("%s [ALERT] %s/%s not ready within timeout\n",
			c.color(colorRed, time.Now().Format(time.RFC3339)),
			event.Namespace, event.PodName)
	case types.PodStatusError:
		c.summary.podError++
		fmt.Printf("%s [ERROR] %s/%s: %s\n",
			c.color(colorRed, time.Now().Format(time.RFC3339)),
			event.Namespace, event.PodName, event.Message)
	case types.PodStatusOK:
		c.summary.podOK++
		msg := event.Message
		if msg == "" {
			msg = fmt.Sprintf("%s/%s no errors in log scan", event.Namespace, event.PodName)
		}
		fmt.Printf("%s [OK] %s\n",
			c.color(colorGreen, time.Now().Format(time.RFC3339)), msg)
	case types.PodStatusWarning:
		fmt.Printf("%s [WARN] %s/%s: %s\n",
			c.color(colorYellow, time.Now().Format(time.RFC3339)),
			event.Namespace, event.PodName, event.Message)
	}
}

// OnLogMatch prints a matched log line.
func (c *ConsoleNotifier) OnLogMatch(match types.LogMatch) {
	c.mu.Lock()
	defer c.mu.Unlock()

	prefix := fmt.Sprintf("%s/%s", match.Namespace, match.PodName)
	if match.Container != "" {
		prefix += "/" + match.Container
	}
	fmt.Printf("%s [ALERT] %s keyword=%q: %s\n",
		c.color(colorRed, time.Now().Format(time.RFC3339)),
		prefix, match.Keyword, match.Line)
}

// OnStart prints the startup banner.
func (c *ConsoleNotifier) OnStart(ns, selector string) {
	fmt.Printf("%s kubectl-checkpods started\n", c.color(colorCyan, time.Now().Format(time.RFC3339)))
	if ns != "" {
		fmt.Printf("  namespace: %s\n", ns)
	}
	if selector != "" {
		fmt.Printf("  selector:  %s\n", selector)
	}
}

// OnExit prints the final summary and test report.
func (c *ConsoleNotifier) OnExit(result types.MonitorResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var passed, failed []types.DeploymentEvent
	for _, d := range result.Deployments {
		if d.PodsError > 0 {
			failed = append(failed, d)
		} else {
			passed = append(passed, d)
		}
	}

	fmt.Println()
	fmt.Println("=" + stringsRepeat("=", 58))

	if len(passed) > 0 {
		fmt.Printf("  Passed Deployments (%d):\n", len(passed))
		for _, d := range passed {
			fmt.Printf("    %s/%s  %s  (replicas: %d/%d ready)\n",
				d.Namespace, d.Name,
				c.color(colorGreen, "PASS"),
				d.ReadyReplicas, d.DesiredReplicas)
		}
	}

	if len(failed) > 0 {
		fmt.Printf("  Failed Deployments (%d):\n", len(failed))
		for _, d := range failed {
			fmt.Printf("    %s/%s  %s  (replicas: %d/%d ready, %d pod(s) with errors)\n",
				d.Namespace, d.Name,
				c.color(colorRed, "FAIL"),
				d.ReadyReplicas, d.DesiredReplicas, d.PodsError)
		}
	}

	totalStatus := "ALL PASS"
	totalClr := colorGreen
	if len(failed) > 0 {
		totalStatus = fmt.Sprintf("%d FAILED", len(failed))
		totalClr = colorRed
	}

	fmt.Printf("  ---\n")
	fmt.Printf("  Result: %s | %d passed / %d failed / %d deployments\n",
		c.color(totalClr, totalStatus), len(passed), len(failed), len(result.Deployments))
	fmt.Printf("  Duration: %s\n", result.Duration.Round(time.Second))
	fmt.Println("=" + stringsRepeat("=", 58))
}

// StringsRepeat is a helper to repeat a string.
func stringsRepeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
