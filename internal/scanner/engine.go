package scanner

import (
	"bufio"
	"context"
	"fmt"
	"time"

	"kubectl-checkpods/internal/config"
	"kubectl-checkpods/pkg/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ScanCallbacks are invoked when the scanner finds matched or errors.
type ScanCallbacks interface {
	OnMatch(match types.LogMatch)
	OnScanError(ns, pod, container string, err error)
	OnScanComplete(ns, pod string, container string, matches int)
}

// Engine scans pod container logs for error patterns.
type Engine struct {
	clientset kubernetes.Interface
	matcher   *Matcher
	dedup     *DedupWindow
	callbacks ScanCallbacks
	tailLines int64
	logDuration time.Duration
}

// NewEngine creates a new scanner engine.
func NewEngine(clientset kubernetes.Interface, cfg *config.Config, callbacks ScanCallbacks) *Engine {
	return &Engine{
		clientset:   clientset,
		matcher:     NewMatcher(cfg),
		dedup:       NewDedupWindow(5 * time.Second),
		callbacks:   callbacks,
		tailLines:   cfg.TailLines,
		logDuration: cfg.LogDuration,
	}
}

// ScanPodLogs scans logs for all containers in a pod.
// Returns the total number of matches found across all containers.
func (e *Engine) ScanPodLogs(ctx context.Context, ns, podName, deployment string) (int, error) {
	pod, err := e.clientset.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to get pod: %w", err)
	}

	type result struct {
		container string
		matches   int
		err       error
	}

	results := make(chan result, len(pod.Spec.Containers))

	for _, c := range pod.Spec.Containers {
		go func(container corev1.Container) {
			matches, err := e.scanContainer(ctx, ns, podName, deployment, container.Name)
			results <- result{container: container.Name, matches: matches, err: err}
		}(c)
	}

	var totalMatches int
	for i := 0; i < cap(results); i++ {
		r := <-results
		if r.err != nil {
			e.callbacks.OnScanError(ns, podName, r.container, r.err)
		}
		totalMatches += r.matches
		e.callbacks.OnScanComplete(ns, podName, r.container, r.matches)
	}

	return totalMatches, nil
}

func (e *Engine) scanContainer(ctx context.Context, ns, podName, deployment, containerName string) (int, error) {
	logOpts := &corev1.PodLogOptions{
		Follow:    true,
		TailLines: &e.tailLines,
		Container: containerName,
	}

	req := e.clientset.CoreV1().Pods(ns).GetLogs(podName, logOpts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return 0, fmt.Errorf("stream failed: %w", err)
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lineNum int64
	matchCount := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		mr := e.matcher.Match(line)
		if mr.Excluded {
			continue
		}
		if !mr.Matched {
			continue
		}

		match := BuildLogMatch(ns, podName, containerName, deployment, mr.Keyword, line, lineNum)
		e.callbacks.OnMatch(match)
		matchCount++
	}

	if err := scanner.Err(); err != nil {
		return matchCount, fmt.Errorf("scan error: %w", err)
	}

	return matchCount, nil
}
