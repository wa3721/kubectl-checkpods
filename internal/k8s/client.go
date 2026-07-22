package k8s

import (
	"fmt"
	"path/filepath"

	"kubectl-checkpods/internal/config"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Client wraps a Kubernetes clientset and informer factories.
type Client struct {
	CS kubernetes.Interface

	// Label selector parsed from config
	LabelSelector labels.Selector

	// Informer factory for Pods (optionally scoped to namespace)
	PodInformerFactory informers.SharedInformerFactory

	// Informer factory for Deployments (optionally scoped to namespace)
	DeployInformerFactory informers.SharedInformerFactory
}

// NewClient creates a new Kubernetes client from the given configuration.
func NewClient(cfg *config.Config) (*Client, error) {
	kubeconfig := cfg.Kubeconfig
	if kubeconfig == "" {
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}

	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	cs, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	sel := labels.Everything()
	if cfg.Selector != "" {
		sel, err = labels.Parse(cfg.Selector)
		if err != nil {
			return nil, fmt.Errorf("failed to parse label selector: %w", err)
		}
	}

	tweak := func(opts *metav1.ListOptions) {
		opts.LabelSelector = sel.String()
	}

	var podFactory, deployFactory informers.SharedInformerFactory
	if cfg.Namespace != "" {
		podFactory = informers.NewSharedInformerFactoryWithOptions(
			cs, 0,
			informers.WithNamespace(cfg.Namespace),
			informers.WithTweakListOptions(tweak),
		)
		deployFactory = informers.NewSharedInformerFactoryWithOptions(
			cs, 0,
			informers.WithNamespace(cfg.Namespace),
			informers.WithTweakListOptions(tweak),
		)
	} else {
		podFactory = informers.NewSharedInformerFactoryWithOptions(
			cs, 0,
			informers.WithTweakListOptions(tweak),
		)
		deployFactory = informers.NewSharedInformerFactoryWithOptions(
			cs, 0,
			informers.WithTweakListOptions(tweak),
		)
	}

	return &Client{
		CS:                   cs,
		LabelSelector:        sel,
		PodInformerFactory:   podFactory,
		DeployInformerFactory: deployFactory,
	}, nil
}
