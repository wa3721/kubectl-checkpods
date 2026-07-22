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
		containers := ""
		if len(event.Containers) > 0 {
			containers = fmt.Sprintf(", containers: %v", event.Containers)
		}
		fmt.Printf("%s [OK] %s/%s no errors in log scan%s\n",
			c.color(colorGreen, time.Now().Format(time.RFC3339)),
			event.Namespace, event.PodName, containers)
	case types.PodStatusDeleted:
		fmt.Printf("%s [INFO] %s/%s deleted\n",
			c.color(colorYellow, time.Now().Format(time.RFC3339)),
			event.Namespace, event.PodName)
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

	fmt.Println()
	fmt.Println("=" + stringsRepeat("=", 58))

	// Per-deployment summary
	for _, d := range result.Deployments {
		status := "PASS"
		clr := colorGreen
		if d.PodsError > 0 {
			status = "FAIL"
			clr = colorRed
		}
		fmt.Printf("  Deployment: %s/%s  %s\n",
			d.Namespace, d.Name,
			c.color(clr, status))
		fmt.Printf("    Replicas: %d desired / %d ready / %d available\n",
			d.DesiredReplicas, d.ReadyReplicas, d.AvailableReplicas)
		fmt.Printf("    Pods: %d ok / %d error / %d total\n",
			d.PodsOK, d.PodsError, d.PodsTotal)
	}

	// Overall summary
	totalStatus := "PASS"
	totalClr := colorGreen
	if result.HasErrors {
		totalStatus = "FAIL"
		totalClr = colorRed
	}

	fmt.Printf("  ---\n")
	fmt.Printf("  Total: %s | pods: %d ok / %d error / %d timeout / %d total\n",
		c.color(totalClr, totalStatus),
		result.PodsOK, result.PodsError, result.PodsTimeout, result.PodsTotal)
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
