package notifier

import (
	"kubectl-checkpods/pkg/types"
)

// Notifier is the interface for processing monitor events.
type Notifier interface {
	// OnDeploymentEvent is called when a deployment phase changes.
	OnDeploymentEvent(event types.DeploymentEvent)

	// OnPodEvent is called when a pod lifecycle event occurs.
	OnPodEvent(event types.PodEvent)

	// OnLogMatch is called when a log line matches a pattern.
	OnLogMatch(match types.LogMatch)

	// OnStart is called when monitoring begins.
	OnStart(ns, selector string)

	// OnExit is called when monitoring ends.
	OnExit(result types.MonitorResult)
}

// MultiNotifier distributes events to multiple notifiers.
type MultiNotifier struct {
	notifiers []Notifier
}

// NewMultiNotifier creates a notifier that fans out events.
func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

func (m *MultiNotifier) OnDeploymentEvent(event types.DeploymentEvent) {
	for _, n := range m.notifiers {
		n.OnDeploymentEvent(event)
	}
}

func (m *MultiNotifier) OnPodEvent(event types.PodEvent) {
	for _, n := range m.notifiers {
		n.OnPodEvent(event)
	}
}

func (m *MultiNotifier) OnLogMatch(match types.LogMatch) {
	for _, n := range m.notifiers {
		n.OnLogMatch(match)
	}
}

func (m *MultiNotifier) OnStart(ns, selector string) {
	for _, n := range m.notifiers {
		n.OnStart(ns, selector)
	}
}

func (m *MultiNotifier) OnExit(result types.MonitorResult) {
	for _, n := range m.notifiers {
		n.OnExit(result)
	}
}
