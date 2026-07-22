package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"kubectl-checkpods/internal/config"
	"kubectl-checkpods/internal/k8s"
	"kubectl-checkpods/internal/monitor"
	"kubectl-checkpods/internal/notifier"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/util/homedir"
)

const version = "0.2.0"

func main() {
	runtime.ErrorHandlers = append(runtime.ErrorHandlers, func(ctx context.Context, err error, msg string, keysAndValues ...interface{}) {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	})

	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var (
		kubeconfig    string
		namespace     string
		selector      string
		readyTimeout  time.Duration
		logDuration   time.Duration
		tailLines     int64
		keywords      []string
		excludes      []string
		noColor       bool
		jsonOutput    bool
		maxWorkers    int
		exitOnComplete bool
	)

	cmd := &cobra.Command{
		Use:     "kubectl-checkpods",
		Version: version,
		Short:   "Monitor Deployment rolling updates and scan pod logs for errors",
		Long: `kubectl-checkpods is a kubectl plugin for automated monitoring of Deployment
rolling updates. It watches for pod lifecycle changes, waits for readiness,
then scans container logs for error patterns.

It is designed for CI/CD post-release validation: run it after or during
a deployment rollout to detect issues automatically.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := &config.Config{
				Kubeconfig:      kubeconfig,
				Namespace:       namespace,
				Selector:        selector,
				ReadyTimeout:    readyTimeout,
				LogDuration:     logDuration,
				TailLines:       tailLines,
				Keywords:        keywords,
				Excludes:        excludes,
				NoColor:         noColor,
				JSONOutput:      jsonOutput,
				MaxWorkers:      maxWorkers,
				DeploymentsOnly: true,
				ExitOnComplete:  exitOnComplete,
			}

			// Resolve kubeconfig default
			if cfg.Kubeconfig == "" {
				if home := homedir.HomeDir(); home != "" {
					cfg.Kubeconfig = filepath.Join(home, ".kube", "config")
				}
			}

			cfg.ApplyDefaults()
			if err := cfg.Validate(); err != nil {
				return err
			}

			client, err := k8s.NewClient(cfg)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			n := notifier.NewConsoleNotifier(!cfg.NoColor)
			engine := monitor.NewEngine(client, cfg, n)

			if err := engine.Run(ctx); err != nil {
				return err
			}

			// Exit with code 1 if errors were found (for CI integration)
			if engine.Summary().HasErrors {
				os.Exit(1)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file")

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Only monitor the specified namespace")
	cmd.Flags().StringVarP(&selector, "selector", "l", "", "Label selector filter (e.g. app=nginx)")

	cmd.Flags().DurationVar(&readyTimeout, "ready-timeout", 3*time.Minute, "Timeout waiting for pod readiness")
	cmd.Flags().DurationVar(&logDuration, "log-duration", 2*time.Minute, "Duration to scan pod logs")
	cmd.Flags().Int64Var(&tailLines, "tail", 100, "Number of recent log lines to start scanning from")

	cmd.Flags().StringSliceVar(&keywords, "keywords", nil, "Log keywords to match (default: error,fatal)")
	cmd.Flags().StringSliceVar(&excludes, "exclude", nil, "Patterns to exclude from matching")

	cmd.Flags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().IntVar(&maxWorkers, "workers", 10, "Maximum concurrent pod processors")
	cmd.Flags().BoolVar(&exitOnComplete, "exit-on-complete", false, "Exit when all deployments complete")

	return cmd
}
