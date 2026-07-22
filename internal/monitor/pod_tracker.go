package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"kubectl-checkpods/pkg/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// podTracker handles the lifecycle of a single pod during monitoring.
type podTracker struct {
	clientset    kubernetes.Interface
	namespace    string
	podName      string
	deployment   string
	readyTimeout time.Duration
	startTime    time.Time
	doneCh       chan struct{}
}

// podResult is the outcome of tracking a pod.
type podResult struct {
	ns         string
	podName    string
	deployment string
	ok         bool
	err        error
	event      types.PodEvent
}

// newPodTracker creates a pod tracker.
func newPodTracker(clientset kubernetes.Interface, ns, name, deployment string, readyTimeout time.Duration) *podTracker {
	return &podTracker{
		clientset:    clientset,
		namespace:    ns,
		podName:      name,
		deployment:   deployment,
		readyTimeout: readyTimeout,
		startTime:    time.Now(),
		doneCh:       make(chan struct{}),
	}
}

// waitForReady polls the pod until it becomes ready or the context is cancelled.
func (pt *podTracker) waitForReady(ctx context.Context) *podResult {
	result := &podResult{
		ns:         pt.namespace,
		podName:    pt.podName,
		deployment: pt.deployment,
	}

	ctx, cancel := context.WithTimeout(ctx, pt.readyTimeout)
	defer cancel()

	err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		pod, err := pt.clientset.CoreV1().Pods(pt.namespace).Get(ctx, pt.podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})

	if err != nil {
		switch err {
		case context.DeadlineExceeded:
			result.ok = false
			result.event = types.PodEvent{
				Timestamp:    time.Now(),
				Namespace:    pt.namespace,
				PodName:      pt.podName,
				Deployment:   pt.deployment,
				Status:       types.PodStatusTimeout,
				ReadyLatency: pt.readyTimeout,
				Message:      fmt.Sprintf("not ready within %v", pt.readyTimeout),
			}
		case context.Canceled:
			return nil
		default:
			result.ok = false
			result.event = types.PodEvent{
				Timestamp:  time.Now(),
				Namespace:  pt.namespace,
				PodName:    pt.podName,
				Deployment: pt.deployment,
				Status:     types.PodStatusDeleted,
				Message:    err.Error(),
			}
		}
		return result
	}

	result.ok = true
	result.event = types.PodEvent{
		Timestamp:    time.Now(),
		Namespace:    pt.namespace,
		PodName:      pt.podName,
		Deployment:   pt.deployment,
		Status:       types.PodStatusReady,
		ReadyLatency: time.Since(pt.startTime),
	}
	return result
}

// done signals that tracking is complete.
func (pt *podTracker) done() {
	close(pt.doneCh)
}

// key returns the unique identifier for this pod.
func (pt *podTracker) key() string {
	return pt.namespace + "/" + pt.podName
}

// workerPool limits concurrent goroutines for pod processing.
type workerPool struct {
	sem chan struct{}
	wg  sync.WaitGroup
}

// newWorkerPool creates a worker pool with the given concurrency limit.
func newWorkerPool(maxWorkers int) *workerPool {
	return &workerPool{
		sem: make(chan struct{}, maxWorkers),
	}
}

// Submit runs fn in a goroutine, respecting the concurrency limit.
func (wp *workerPool) Submit(fn func()) {
	wp.wg.Add(1)
	go func() {
		wp.sem <- struct{}{}
		defer func() { <-wp.sem }()
		defer wp.wg.Done()
		fn()
	}()
}

// Wait blocks until all workers have completed.
func (wp *workerPool) Wait() {
	wp.wg.Wait()
}
