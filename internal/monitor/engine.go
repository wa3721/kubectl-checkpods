package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"kubectl-checkpods/internal/config"
	"kubectl-checkpods/internal/k8s"
	"kubectl-checkpods/internal/notifier"
	"kubectl-checkpods/internal/scanner"
	"kubectl-checkpods/pkg/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
)

// Engine is the main monitoring engine that coordinates all subsystems.
type Engine struct {
	client    *k8s.Client
	cfg       *config.Config
	notifier  notifier.Notifier
	scanner   *scanner.Engine

	// Deployment tracking
	deployments map[string]*deploymentState
	deployMu    sync.RWMutex

	// Pod tracking
	tracking map[string]*podTracker
	trackMu  sync.Mutex

	// Worker pool
	pool *workerPool

	// Results
	result types.MonitorResult
	resultMu sync.Mutex

	// Global context for cancellation propagation
	ctx context.Context

	// Startup time to filter pre-existing pods
	startupTime time.Time
}

// NewEngine creates a new monitor engine.
func NewEngine(client *k8s.Client, cfg *config.Config, n notifier.Notifier) *Engine {
	e := &Engine{
		client:      client,
		cfg:         cfg,
		notifier:    n,
		deployments: make(map[string]*deploymentState),
		tracking:    make(map[string]*podTracker),
		pool:        newWorkerPool(cfg.MaxWorkers),
		startupTime: time.Now(),
	}
	e.scanner = scanner.NewEngine(client.CS, cfg, e)
	return e
}

// Run starts the monitoring engine and blocks until the context is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	e.ctx = ctx
	e.result.StartTime = time.Now()
	e.notifier.OnStart(e.cfg.Namespace, e.cfg.Selector)

	// Register informer event handlers
	e.registerPodInformer(ctx)
	e.registerDeploymentInformer(ctx)

	// Start informers
	e.client.PodInformerFactory.Start(ctx.Done())
	e.client.DeployInformerFactory.Start(ctx.Done())

	if !cache.WaitForCacheSync(ctx.Done(),
		e.client.PodInformerFactory.Core().V1().Pods().Informer().HasSynced,
		e.client.DeployInformerFactory.Apps().V1().Deployments().Informer().HasSynced,
	) {
		return fmt.Errorf("informer cache sync failed")
	}

	// Block until done
	<-ctx.Done()

	// Wait for all in-flight processing to complete
	e.pool.Wait()

	e.result.EndTime = time.Now()
	e.result.Duration = e.result.EndTime.Sub(e.result.StartTime)

	// Collect deployment results
	e.deployMu.RLock()
	for _, ds := range e.deployments {
		e.result.Deployments = append(e.result.Deployments, ds.toEvent())
	}
	e.deployMu.RUnlock()

	e.result.HasErrors = e.result.PodsError > 0 || e.result.PodsTimeout > 0
	e.notifier.OnExit(e.result)

	return nil
}

func (e *Engine) registerPodInformer(ctx context.Context) {
	informer := e.client.PodInformerFactory.Core().V1().Pods().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				return
			}
			if pod.CreationTimestamp.Time.Before(e.startupTime) {
				return
			}
			e.onNewPod(pod)
		},
		UpdateFunc: func(_, newObj interface{}) {
			pod, ok := newObj.(*corev1.Pod)
			if !ok {
				return
			}
			// Same filter as Add: only pods created after startup
			if pod.CreationTimestamp.Time.Before(e.startupTime) {
				return
			}
			e.onNewPod(pod)
		},
	})
}

func (e *Engine) registerDeploymentInformer(ctx context.Context) {
	informer := e.client.DeployInformerFactory.Apps().V1().Deployments().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			deploy, ok := obj.(*appsv1.Deployment)
			if !ok {
				return
			}
			e.onDeploymentUpdate(deploy)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			deploy, ok := newObj.(*appsv1.Deployment)
			if !ok {
				return
			}
			e.onDeploymentUpdate(deploy)
		},
	})
}

func (e *Engine) onDeploymentUpdate(deploy *appsv1.Deployment) {
	key := deploy.Namespace + "/" + deploy.Name
	e.deployMu.Lock()
	ds, exists := e.deployments[key]
	if !exists {
		ds = newDeploymentState(deploy)
		e.deployments[key] = ds
	}
	prevPhase := ds.phase
	ds.updateFromDeployment(deploy)
	e.deployMu.Unlock()

	if ds.phase != prevPhase {
		e.notifier.OnDeploymentEvent(ds.toEvent())
	}
}

func (e *Engine) onNewPod(pod *corev1.Pod) {
	key := pod.Namespace + "/" + pod.Name

	e.trackMu.Lock()
	if _, exists := e.tracking[key]; exists {
		e.trackMu.Unlock()
		return
	}

	// Resolve owner deployment
	deployment := resolveDeploymentOwner(pod)

	// Register with deployment tracker
	e.deployMu.Lock()
	deployKey := pod.Namespace + "/" + deployment
	if ds, ok := e.deployments[deployKey]; ok {
		ds.addPod(pod.Name)
	}
	e.deployMu.Unlock()

	pt := newPodTracker(e.client.CS, pod.Namespace, pod.Name, deployment, e.cfg.ReadyTimeout)
	e.tracking[key] = pt
	e.trackMu.Unlock()

	e.pool.Submit(func() {
		e.trackPod(pt)
	})
}

func (e *Engine) trackPod(pt *podTracker) {
	defer func() {
		e.trackMu.Lock()
		delete(e.tracking, pt.key())
		e.trackMu.Unlock()
		pt.done()
	}()

	// Phase 1: Wait for ready
	result := pt.waitForReady(e.ctx)
	if result == nil {
		return
	}

	// Pod was deleted during tracking — normal rolling update, skip silently.
	// Don't even print anything; only newly created pods matter.
	if result.event.Status == types.PodStatusDeleted {
		return
	}

	e.notifier.OnPodEvent(result.event)

	if !result.ok {
		// Pod timed out waiting for ready: deployment FAIL.
		e.updateResult(false, true)
		e.recordDeploymentResult(pt.deployment, pt.podName, false)
		return
	}

	// Phase 2: Scan logs
	scanCtx, scanCancel := context.WithTimeout(e.ctx, e.cfg.LogDuration)
	defer scanCancel()

	e.notifier.OnPodEvent(types.PodEvent{
		Timestamp:  time.Now(),
		Namespace:  pt.namespace,
		PodName:    pt.podName,
		Deployment: pt.deployment,
		Status:     types.PodStatusScanning,
		Message:    fmt.Sprintf("%s/%s 开始扫描日志", pt.namespace, pt.podName),
	})

	totalMatches, err := e.scanner.ScanPodLogs(scanCtx, pt.namespace, pt.podName, pt.deployment)
	if err != nil {
		runtime.HandleError(fmt.Errorf("scan logs for %s/%s: %w", pt.namespace, pt.podName, err))
	}

	if totalMatches > 0 {
		e.updateResult(false, false)
		e.recordDeploymentResult(pt.deployment, pt.podName, false)
		e.notifier.OnPodEvent(types.PodEvent{
			Timestamp:  time.Now(),
			Namespace:  pt.namespace,
			PodName:    pt.podName,
			Deployment: pt.deployment,
			Status:     types.PodStatusError,
			Message:    fmt.Sprintf("found %d error(s) in logs", totalMatches),
		})
	} else {
		e.updateResult(true, false)
		e.recordDeploymentResult(pt.deployment, pt.podName, true)
		msg := fmt.Sprintf("%s/%s: %s 内未检测到错误日志，Deployment %s 已就绪",
			pt.namespace, pt.podName, e.cfg.LogDuration.Round(time.Second), pt.deployment)
		e.notifier.OnPodEvent(types.PodEvent{
			Timestamp:    time.Now(),
			Namespace:    pt.namespace,
			PodName:      pt.podName,
			Deployment:   pt.deployment,
			Status:       types.PodStatusOK,
			ReadyLatency: time.Since(pt.startTime),
			Message:      msg,
		})
	}
}

func (e *Engine) updateResult(ok bool, timedOut bool) {
	e.resultMu.Lock()
	defer e.resultMu.Unlock()
	if timedOut {
		e.result.PodsTimeout++
	} else if ok {
		e.result.PodsOK++
	} else {
		e.result.PodsError++
	}
}

func (e *Engine) recordDeploymentResult(deployName, podName string, ok bool) {
	if deployName == "" {
		return
	}
	e.deployMu.Lock()
	defer e.deployMu.Unlock()
	// Find all deployments that match and record
	for _, ds := range e.deployments {
		if ds.name == deployName {
			ds.recordPodResult(podName, ok)
			e.notifier.OnDeploymentEvent(ds.toEvent())
		}
	}
}

// resolveDeploymentOwner resolves a pod's owning Deployment via ownerReferences.
func resolveDeploymentOwner(pod *corev1.Pod) string {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "ReplicaSet" {
			// ReplicaSet name is typically "deployName-podTemplateHash"
			// The deployment name is everything before the last '-'
			name := owner.Name
			lastDash := len(name)
			for i := len(name) - 1; i >= 0; i-- {
				if name[i] == '-' {
					lastDash = i
					break
				}
			}
			if lastDash > 0 {
				return name[:lastDash]
			}
		}
	}
	return ""
}

// ScanCallbacks implementation for scanner.ScanCallbacks.
func (e *Engine) OnMatch(match types.LogMatch) {
	e.notifier.OnLogMatch(match)
}

func (e *Engine) OnScanError(ns, pod, container string, err error) {
	e.notifier.OnPodEvent(types.PodEvent{
		Timestamp: time.Now(),
		Namespace: ns,
		PodName:   pod,
		Status:    types.PodStatusWarning,
		Message:   fmt.Sprintf("[%s] %v", container, err),
	})
}

func (e *Engine) OnScanComplete(ns, pod, container string, matches int) {
	// No-op for now; summary is handled at the pod level
}

// Summary returns a copy of the current monitor result.
func (e *Engine) Summary() types.MonitorResult {
	e.resultMu.Lock()
	defer e.resultMu.Unlock()
	return e.result
}
