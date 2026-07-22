# kubectl-checkpods

A kubectl plugin for automated monitoring of Deployment rolling updates and pod log error scanning.

Designed for post-release validation in CI/CD pipelines: runs after a deployment rollout and reports whether all pods started cleanly.

## Features

- **Deployment-aware**: watches Deployment objects to detect rolling update start/completion
- **Pod lifecycle tracking**: waits for each pod to become Ready, with configurable timeout
- **Intelligent log scanning**: follow-mode log streaming with keyword + exclude pattern matching
- **Multi-container**: scans all containers in a pod, deduplicates repeated errors
- **Worker pool**: controls concurrent pod processing to avoid resource exhaustion
- **CI integration**: exits with code 1 when errors are found, 0 when all pods pass
- **Graceful shutdown**: Ctrl+C cancels all in-flight operations cleanly

## Architecture

```
cmd/kubectl-checkpods/    CLI entry point (thin)
internal/
  config/                 Configuration management
  k8s/                    Kubernetes client + informer factory
  monitor/                Core engine (deployment tracker + pod tracker + worker pool)
  scanner/                Log scanning engine (pattern matching + dedup)
  notifier/               Notification interface (console + future webhook/slack)
pkg/types/                Shared type definitions
```

## Quick Start

```bash
git clone https://github.com/wa3721/kubectl-checkpods.git
cd kubectl-checkpods
make build
```

## Usage

### Basic

```bash
kubectl checkpods -n production -l app=myapp
```

### CI/CD Pipeline

```bash
kubectl checkpods -n production -l app=myapp --no-color --exit-on-complete
# exit 0 = all pods OK
# exit 1 = errors found
```

### Exclude false positives

```bash
kubectl checkpods -n production \
  --keywords error,fatal,panic \
  --exclude "errorCode=0" --exclude "no error" \
  --ready-timeout 5m --log-duration 3m
```

## Options

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--kubeconfig` | | `~/.kube/config` | Path to kubeconfig |
| `--namespace` | `-n` | all | Only monitor this namespace |
| `--selector` | `-l` | all | Label selector filter |
| `--ready-timeout` | | `3m` | Timeout for pod readiness |
| `--log-duration` | | `2m` | Duration to scan logs |
| `--tail` | | `100` | Recent lines to start from |
| `--keywords` | | `error,fatal` | Keywords to match |
| `--exclude` | | | Patterns to exclude |
| `--workers` | | `10` | Max concurrent pod processors |
| `--no-color` | | false | Disable colored output |
| `--exit-on-complete` | | false | Exit when all deployments complete |

## Output Example

```
2026-07-22T22:37:07+08:00 kubectl-checkpods started
  namespace: production
  selector:  app=myapp
[DEPLOY] production/myapp rollout started (desired: 5)
[READY] production/myapp-7d4f8b9c-abc12 (deploy: myapp, took 45s)
[READY] production/myapp-7d4f8b9c-def34 (deploy: myapp, took 52s)
[ALERT] production/myapp-7d4f8b9c-ghi56 keyword="error": Connection refused: dial tcp 10.0.0.5:8080
[DEPLOY] production/myapp rollout complete (5/5 ready, 4 ok, 1 errors)

==========================================================
  Deployment: production/myapp  FAIL
    Replicas: 5 desired / 5 ready / 5 available
    Pods: 4 ok / 1 error / 5 total
  ---
  Total: FAIL | pods: 4 ok / 1 error / 0 timeout / 5 total
  Duration: 3m45s
==========================================================
```

## Development

```bash
make build      # Build binary
make clean      # Remove build artifacts
go vet ./...    # Code quality check
```

## License

MIT
