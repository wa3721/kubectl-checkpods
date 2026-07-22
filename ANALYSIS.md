# kubectl-checkpods 重构总结

## 重构目标

针对"Kubernetes 发版后滚动更新自动监控"场景，将原有单文件 Pod 级监控重构为标准 Go 项目结构，补齐四个核心能力短板。

## 新增核心能力

### 1. Deployment 级别的滚动更新感知

| 原有能力 | 新增能力 |
|----------|----------|
| 仅监听 Pod Add/Update | 同时监听 Deployment Add/Update |
| 不知道 Pod 属于哪个 Deployment | 通过 ownerReferences 自动关联 ReplicaSet → Deployment |
| 无滚动更新进度 | 追踪 updatedReplicas/readyReplicas/availableReplicas |
| 无完成状态判断 | 自动检测 rollout 开始/进行中/完成/失败 |

### 2. 日志检测准确性

| 原有能力 | 新增能力 |
|----------|----------|
| `strings.Contains` 简单匹配 | 支持 Regex + 子串匹配双模式 |
| "error" 匹配 "errorCode=0" | 新增 `--exclude` 排除模式 |
| 无去重 | 5 秒时间窗口内相同错误去重 |
| 无结构化解析 | API 接口预留 JSON 日志字段级解析 |

### 3. 异常通知及时性

| 原有能力 | 新增能力 |
|----------|----------|
| 仅 stdout 打印 | Notifier 接口，Console 实现，可扩展 Webhook |
| 始终 exit 0 | 有错误时 exit 1，CI/CD 可直接感知 |
| 无汇总 | Per-Deployment 测试报告 + 整体 PASS/FAIL 判定 |

### 4. 整体流程健壮性

| 原有能力 | 新增能力 |
|----------|----------|
| 无并发控制 | Worker Pool 限制并发 goroutine 数量 |
| 无重试 | API 调用使用 wait.Poll 内置重试 |
| 无超时分层 | readyTimeout + logDuration + 全局 ctx 三级超时 |
| 单文件 300+ 行 | cmd/internal/pkg 标准分层，可按包测试 |

## 项目结构

```
kubectl-checkpods/
├── cmd/
│   └── kubectl-checkpods/
│       └── main.go            # CLI 入口，组装依赖
├── internal/
│   ├── config/
│   │   └── config.go          # 配置管理 + 校验
│   ├── k8s/
│   │   └── client.go          # K8s 客户端 + Informer 工厂
│   ├── monitor/
│   │   ├── engine.go          # 核心引擎，协调各模块
│   │   ├── deployment.go      # Deployment 状态追踪
│   │   └── pod_tracker.go     # Pod 生命周期追踪 + Worker Pool
│   ├── scanner/
│   │   ├── engine.go          # 日志扫描引擎
│   │   └── matcher.go         # 模式匹配 + 去重
│   └── notifier/
│       ├── notifier.go        # Notifier 接口 + MultiNotifier
│       └── event.go           # Console 实现 + 汇总
├── pkg/
│   └── types/
│       └── types.go           # 共享类型定义
├── go.mod
├── makefile
└── README.md
```

## 使用流程

```
发版触发
  ↓
kubectl checkpods -n production -l app=myapp --exclude "errorCode=0"
  ↓
Deployment Informer 感知 rollout 开始
  ├─ Pod Informer 捕获新 Pod (ownerRef → ReplicaSet → Deployment)
  ├─ Worker Pool 并发等待每个 Pod 就绪
  ├─ Scanner 并行扫描多容器日志 (regex + exclude 过滤 + 去重)
  └─ Notifier 实时告警 + 最终汇总
  ↓
exit 0 = 全部通过, exit 1 = 有错误
```

## 关键设计决策

1. **Notifier 接口抽象**：Console 只是第一个实现，未来可加 Webhook/Slack/钉钉
2. **Worker Pool 并发控制**：避免同时处理数百个 Pod 时 OOM
3. **Owner 解析启发式**：通过 ReplicaSet 名称推断 Deployment 名称（去尾部 hash）
4. **三级超时**：全局 ctx → readyTimeout → logDuration，Ctrl+C 级联取消
5. **排除模式**：`--exclude` 参数解决 "error" 关键字误报问题
