package monitor

import (
	"sync"
	"time"

	"kubectl-checkpods/pkg/types"

	appsv1 "k8s.io/api/apps/v1"
)

// deploymentState tracks the rolling update state of a single deployment.
type deploymentState struct {
	ns           string
	name         string
	phase        types.DeploymentPhase
	desired      int32
	updated      int32
	ready        int32
	available    int32

	podsTotal  int
	podsOK     int
	podsError  int

	// PodName -> true for tracked pods belonging to this deployment
	trackedPods map[string]struct{}
	mu          sync.Mutex
}

// newDeploymentState creates a tracker for the given deployment.
func newDeploymentState(deploy *appsv1.Deployment) *deploymentState {
	ds := &deploymentState{
		ns:       deploy.Namespace,
		name:     deploy.Name,
		trackedPods: make(map[string]struct{}),
	}
	ds.updateFromDeployment(deploy)
	return ds
}

// updateFromDeployment refreshes replica counts and detects phase transitions.
func (ds *deploymentState) updateFromDeployment(deploy *appsv1.Deployment) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.desired   = *deploy.Spec.Replicas
	ds.updated   = deploy.Status.UpdatedReplicas
	ds.ready     = deploy.Status.ReadyReplicas
	ds.available = deploy.Status.AvailableReplicas

	// Detect rollout in progress
	if ds.updated < ds.desired || ds.ready < ds.desired || ds.available < ds.desired {
		if ds.phase == types.PhaseIdle {
			ds.phase = types.PhaseInProgress
		}
	} else if ds.phase == types.PhaseInProgress {
		// All replicas ready and available -> rollout complete
		if ds.podsError > 0 {
			ds.phase = types.PhaseFailed
		} else {
			ds.phase = types.PhaseComplete
		}
	}
}

// addPod registers a pod as belonging to this deployment.
func (ds *deploymentState) addPod(name string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.trackedPods[name] = struct{}{}
	ds.podsTotal++
}

// recordPodResult updates per-pod statistics.
func (ds *deploymentState) recordPodResult(name string, ok bool) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ok {
		ds.podsOK++
	} else {
		ds.podsError++
	}

	// Recheck phase after all pods complete
	if len(ds.trackedPods) > 0 && ds.podsOK+ds.podsError >= ds.podsTotal {
		if ds.desired == ds.ready && ds.desired == ds.available {
			if ds.podsError > 0 {
				ds.phase = types.PhaseFailed
			} else {
				ds.phase = types.PhaseComplete
			}
		}
	}
}

// toEvent converts the current state to a DeploymentEvent.
func (ds *deploymentState) toEvent() types.DeploymentEvent {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	progress := float64(0)
	if ds.desired > 0 {
		progress = float64(ds.ready) / float64(ds.desired) * 100
	}

	return types.DeploymentEvent{
		Timestamp:         time.Now(),
		Namespace:         ds.ns,
		Name:              ds.name,
		Phase:             ds.phase,
		DesiredReplicas:   ds.desired,
		UpdatedReplicas:   ds.updated,
		ReadyReplicas:     ds.ready,
		AvailableReplicas: ds.available,
		Progress:          progress,
		PodsTotal:         ds.podsTotal,
		PodsOK:            ds.podsOK,
		PodsError:         ds.podsError,
	}
}
