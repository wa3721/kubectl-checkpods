package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var startupTime time.Time // 全局变量，记录插件启动时刻

func init() {
	startupTime = time.Now()
}

const (
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorReset  = "\033[0m"
)

func main() {
	runtime.ErrorHandlers = append(runtime.ErrorHandlers, func(ctx context.Context, err error, msg string, keysAndValues ...interface{}) {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	})
	err := newRootCmd().Execute()
	if err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var readyTimeout, logDuration time.Duration

	cmd := &cobra.Command{
		Use:   "kubectl-pod-check",
		Short: "监视 Pod 就绪状态并扫描日志中的 error",
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载 kubeconfig
			var kubeconfig *string
			if home := homedir.HomeDir(); home != "" {
				kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
			} else {
				kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
			}
			flag.Parse()
			config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
			if err != nil {
				return err
			}
			clientset, err := kubernetes.NewForConfig(config)
			if err != nil {
				return err
			}

			monitor := &podMonitor{
				clientset:    clientset,
				readyTimeout: readyTimeout,
				logDuration:  logDuration,
			}
			return monitor.run()
		},
	}

	cmd.Flags().DurationVar(&readyTimeout, "ready-timeout", 3*time.Minute, "等待 Pod 就绪的超时时间")
	cmd.Flags().DurationVar(&logDuration, "log-duration", 2*time.Minute, "扫描 Pod 日志的持续时间")
	return cmd
}

type podMonitor struct {
	clientset    kubernetes.Interface
	readyTimeout time.Duration // 就绪等待超时
	logDuration  time.Duration // 日志跟踪时长
}

// 用于跟踪单个 Pod 的状态
type trackedPod struct {
	namespace string
	name      string
	startTime time.Time     // Pod 首次观察到的时间（Add 或 Update 触发）
	doneCh    chan struct{} // 关闭后表示该 Pod 的处理已完成
}

func (m *podMonitor) run() error {
	factory := informers.NewSharedInformerFactory(m.clientset, 0) // 0 表示不重新同步
	informer := factory.Core().V1().Pods().Informer()

	// 使用 map 记录正在追踪的 Pod，避免重复处理
	tracking := make(map[string]*trackedPod)
	var mu sync.Mutex

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			// 忽略启动前已存在的 Pod
			if pod.CreationTimestamp.Time.Before(startupTime) {
				return
			}
			m.startTracking(pod, tracking, &mu)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pod := newObj.(*corev1.Pod)
			// 更新事件一定发生在启动后，无需额外过滤
			m.startTracking(pod, tracking, &mu)
		},
	})

	stopCh := make(chan struct{})
	defer close(stopCh)

	go informer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, informer.HasSynced) {
		return fmt.Errorf("failed to sync informer cache")
	}
	fmt.Printf("timestamp is %s. started.", time.Now().Format(time.RFC3339))
	// 阻塞主 goroutine，保持进程运行
	<-stopCh
	return nil
}

func (m *podMonitor) startTracking(pod *corev1.Pod, tracking map[string]*trackedPod, mu sync.Locker) {
	key := pod.Namespace + "/" + pod.Name
	mu.Lock()
	if _, exists := tracking[key]; exists {
		mu.Unlock()
		return // 已经在追踪中
	}
	tp := &trackedPod{
		namespace: pod.Namespace,
		name:      pod.Name,
		startTime: time.Now(),
		doneCh:    make(chan struct{}),
	}
	tracking[key] = tp
	mu.Unlock()

	go m.processPod(tp, tracking, mu)
}

func (m *podMonitor) processPod(tp *trackedPod, tracking map[string]*trackedPod, mu sync.Locker) {
	defer func() {
		mu.Lock()
		delete(tracking, tp.namespace+"/"+tp.name)
		mu.Unlock()
		close(tp.doneCh)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), m.readyTimeout)
	defer cancel()

	// 轮询等待 Pod 就绪
	err := wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		pod, err := m.clientset.CoreV1().Pods(tp.namespace).Get(ctx, tp.name, metav1.GetOptions{})
		if err != nil {
			// Pod 可能已被删除，停止追踪
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
		if err == context.DeadlineExceeded {
			fmt.Printf("%s[ALERT] %s.%s 未能在 %v 内就绪 %s\n", colorRed, tp.namespace, tp.name, m.readyTimeout, colorReset)
		} else {
			fmt.Printf("[INFO] %s.%s 已删除\n", tp.namespace, tp.name)
		}
		return
	}

	// Pod 已就绪，进入日志扫描阶段
	m.scanLogs(tp)
}

func (m *podMonitor) scanLogs(tp *trackedPod) {
	ctx, cancel := context.WithTimeout(context.Background(), m.logDuration)
	defer cancel()

	logOpts := &corev1.PodLogOptions{
		Follow:    true,
		TailLines: int64Ptr(100), // 从最近 100 行开始
	}

	req := m.clientset.CoreV1().Pods(tp.namespace).GetLogs(tp.name, logOpts)
	stream, err := req.Stream(ctx)
	if err != nil {
		fmt.Printf("%s[WARN] %s.%s 获取日志失败: %v%s\n", colorRed, tp.namespace, tp.name, err, colorReset)
		return
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(strings.ToLower(line), "error") || strings.Contains(strings.ToLower(line), "fatal") {
			fmt.Printf("%s[ALERT] %s.%s 日志中包含 error: %s%s\n", colorRed, tp.namespace, tp.name, line, colorReset)
			return
		}
	}

	// 检查是否因超时而退出
	if ctx.Err() == context.DeadlineExceeded {
		fmt.Printf("%s[OK] %s.%s 已就绪，且日志在 %v 内无 error%s\n", colorGreen, tp.namespace, tp.name, m.logDuration, colorReset)
	} else if err := scanner.Err(); err != nil {
		fmt.Printf("%s[WARN] %s.%s 日志读取异常: %v%s\n", colorYellow, tp.namespace, tp.name, err, colorReset)
	}
}

func int64Ptr(i int64) *int64 { return &i }
