package types

import "time"

// EventSeverity represents the severity level of an event.
type EventSeverity string

const (
	SeverityInfo    EventSeverity = "info"
	SeverityWarning EventSeverity = "warning"
	SeverityError   EventSeverity = "error"
)

// PodStatus represents the status of a tracked pod in the update lifecycle.
type PodStatus string

const (
	PodStatusPending    PodStatus = "pending"
	PodStatusWaiting    PodStatus = "waiting"
	PodStatusReady      PodStatus = "ready"
	PodStatusScanning   PodStatus = "scanning"
	PodStatusError      PodStatus = "error"
	PodStatusOK         PodStatus = "ok"
	PodStatusDeleted    PodStatus = "deleted"
	PodStatusTimeout    PodStatus = "timeout"
	PodStatusWarning    PodStatus = "warning"
)

// DeploymentPhase represents the phase of a rolling update.
type DeploymentPhase string

const (
	PhaseIdle       DeploymentPhase = "idle"
	PhaseInProgress DeploymentPhase = "in_progress"
	PhaseComplete   DeploymentPhase = "complete"
	PhaseFailed     DeploymentPhase = "failed"
)

// PodEvent represents a lifecycle event for a tracked pod.
type PodEvent struct {
	Timestamp    time.Time       `json:"timestamp"`
	Namespace    string          `json:"namespace"`
	PodName      string          `json:"pod_name"`
	Deployment   string          `json:"deployment,omitempty"`
	Status       PodStatus       `json:"status"`
	Containers   []string        `json:"containers,omitempty"`
	ReadyLatency time.Duration   `json:"ready_latency,omitempty"`
	Message      string          `json:"message,omitempty"`
}

// DeploymentEvent represents a rolling update event for a deployment.
type DeploymentEvent struct {
	Timestamp        time.Time       `json:"timestamp"`
	Namespace        string          `json:"namespace"`
	Name             string          `json:"name"`
	Phase            DeploymentPhase `json:"phase"`
	DesiredReplicas  int32           `json:"desired_replicas"`
	UpdatedReplicas  int32           `json:"updated_replicas"`
	ReadyReplicas    int32           `json:"ready_replicas"`
	AvailableReplicas int32          `json:"available_replicas"`
	Progress         float64         `json:"progress"`
	PodsTotal        int             `json:"pods_total"`
	PodsOK           int             `json:"pods_ok"`
	PodsError        int             `json:"pods_error"`
	Message          string          `json:"message,omitempty"`
}

// LogMatch represents a matched log line from a container.
type LogMatch struct {
	Timestamp   time.Time `json:"timestamp"`
	Namespace   string    `json:"namespace"`
	PodName     string    `json:"pod_name"`
	Container   string    `json:"container"`
	Deployment  string    `json:"deployment,omitempty"`
	Keyword     string    `json:"keyword"`
	Line        string    `json:"line"`
	LineNumber  int64     `json:"line_number"`
}

// MonitorResult is the final summary of a monitoring session.
type MonitorResult struct {
	StartTime      time.Time          `json:"start_time"`
	EndTime        time.Time          `json:"end_time"`
	Duration       time.Duration      `json:"duration"`
	Deployments    []DeploymentEvent  `json:"deployments"`
	PodsTotal      int                `json:"pods_total"`
	PodsOK         int                `json:"pods_ok"`
	PodsError      int                `json:"pods_error"`
	PodsTimeout    int                `json:"pods_timeout"`
	LogMatches     []LogMatch         `json:"log_matches,omitempty"`
	PodEvents      []PodEvent         `json:"pod_events,omitempty"`
	HasErrors      bool               `json:"has_errors"`
}
