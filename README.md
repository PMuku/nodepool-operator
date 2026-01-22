# NodePool Operator

A Kubernetes Operator that automates worker node pool management — define pools as Custom Resources, and the operator handles node assignment, labeling, tainting, and lifecycle operations to match your desired state.

## Overview

The **NodePool Operator** allows platform administrators to organize Kubernetes worker nodes into logical pools by automatically applying labels and taints to nodes. This enables:

- **Workload isolation**: Separate GPU workloads from general-purpose workloads
- **Resource partitioning**: Dedicate specific nodes to specific teams or applications
- **Declarative node management**: Define node pools as Kubernetes resources instead of manual `kubectl label/taint` commands
- **Automatic reconciliation**: The operator continuously ensures the actual state matches the desired state

### Key Features

| Feature | Description |
|---------|-------------|
| **Declarative Configuration** | Define node pools as Kubernetes Custom Resources |
| **Automatic Node Assignment** | Automatically labels and taints nodes to match pool size |
| **Safe Scale-Down** | Only removes nodes that are cordoned (safe scale-down) |
| **Status Visibility** | View pool status via `kubectl get nodepools` |
| **Event Tracking** | All changes recorded as Kubernetes Events |
| **Finalizer Support** | Clean removal of labels/taints when pool is deleted |

---

## How It Works

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                          │
│                                                                     │
│  ┌──────────────────┐        ┌──────────────────────────────────┐  │
│  │   NodePool CRD   │        │      NodePool Controller         │  │
│  │                  │        │                                  │  │
│  │  - size: 3       │───────▶│  1. Watch NodePool resources     │  │
│  │  - label: pool=a │        │  2. Find eligible worker nodes   │  │
│  │  - nodeSelector  │        │  3. Apply labels and taints      │  │
│  │                  │        │  4. Update status                │  │
│  └──────────────────┘        └──────────────────────────────────┘  │
│                                          │                          │
│                                          ▼                          │
│  ┌───────────┐ ┌───────────┐ ┌───────────┐ ┌───────────┐          │
│  │  Worker   │ │  Worker   │ │  Worker   │ │  Worker   │          │
│  │  Node 1   │ │  Node 2   │ │  Node 3   │ │  Node 4   │          │
│  │           │ │           │ │           │ │           │          │
│  │ pool=a ✓  │ │ pool=a ✓  │ │ pool=a ✓  │ │ (free)    │          │
│  └───────────┘ └───────────┘ └───────────┘ └───────────┘          │
└─────────────────────────────────────────────────────────────────────┘
```

### Reconciliation Logic

1. **Scale Up**: When `currentSize < desiredSize`, the controller finds eligible unassigned nodes and assigns them to the pool
2. **Scale Down**: When `currentSize > desiredSize`, the controller releases **only cordoned nodes** (safe scale-down policy)
3. **Status Tracking**: The controller continuously updates the pool status with current node assignments

---

## Prerequisites

Before deploying the NodePool Operator, ensure you have:

- Kubernetes cluster v1.19+
- `kubectl` CLI configured with cluster admin access
- Docker (for building custom images, optional)
- Go 1.24+ (for development, optional)

---

## Installation

### Option 1: Quick Install (Recommended for Most Users)

```bash
# Clone the repository
git clone https://github.com/PMuku/nodepool-operator.git
cd nodepool-operator

# Install the CRD
make install

# Deploy the operator (uses default image)
make deploy IMG=<your-registry>/nodepool-operator:latest
```

### Option 2: Build and Deploy Your Own Image

```bash
# Build the Docker image
make docker-build IMG=<your-registry>/nodepool-operator:v1.0.0

# Push to your container registry
make docker-push IMG=<your-registry>/nodepool-operator:v1.0.0

# Install CRDs
make install

# Deploy the operator
make deploy IMG=<your-registry>/nodepool-operator:v1.0.0
```

### Option 3: Using Pre-built YAML Bundle

```bash
# Generate the installation bundle
make build-installer IMG=<your-registry>/nodepool-operator:v1.0.0

# Apply the bundle
kubectl apply -f dist/install.yaml
```

### Verify Installation

```bash
# Check that the CRD is installed
kubectl get crd nodepools.nodepool.k8s.local

# Check that the controller is running
kubectl get pods -n nodepool-operator-system

# View controller logs
kubectl logs -n nodepool-operator-system deployment/nodepool-operator-controller-manager -f
```

---

## Usage Guide

### NodePool Specification Reference

```yaml
apiVersion: nodepool.k8s.local/v1
kind: NodePool
metadata:
  name: <pool-name>
  namespace: <namespace>
spec:
  # Required: Number of nodes in this pool
  size: <integer>
  
  # Required: Label to apply to nodes (format: key=value)
  label: "<key>=<value>"
  
  # Optional: Node selector to filter eligible nodes
  nodeSelector:
    <label-key>: <label-value>
  
  # Optional: Taint to apply (format: key=value:effect)
  # If not specified, uses the label key/value with NoSchedule effect
  taint: "<key>=<value>:<effect>"
```

### Status Fields

| Field | Description |
|-------|-------------|
| `phase` | Current pool state: `Ready`, `Pending`, or `Degraded` |
| `currentSize` | Number of nodes currently usable in the pool |
| `desiredSize` | Target number of nodes (from spec.size) |
| `assignedNodes` | List of node names assigned to this pool |
| `message` | Human-readable status description |

---

## Scenarios and Examples

### Scenario 1: Create a General-Purpose Worker Node Pool

Create a pool for general workloads that excludes GPU nodes.

```yaml
# general-pool.yaml
apiVersion: nodepool.k8s.local/v1
kind: NodePool
metadata:
  name: general-pool
  namespace: default
spec:
  size: 5
  label: "workload-type=general"
```

```bash
# Apply the pool
kubectl apply -f general-pool.yaml

# Watch the pool status
kubectl get nodepool general-pool -w

# View detailed status
kubectl describe nodepool general-pool
```

**What happens:**
- The operator finds 5 eligible worker nodes (non-GPU, non-control-plane, not already assigned)
- Applies label `workload-type=general` to each node
- Applies taint `workload-type=general:NoSchedule` to each node
- Adds internal label `nodepool.k8s.local/name=general-pool` to track ownership

---

### Scenario 2: Create a GPU Worker Node Pool

Create a dedicated pool for GPU workloads by targeting nodes with GPU labels.

```yaml
# gpu-pool.yaml
apiVersion: nodepool.k8s.local/v1
kind: NodePool
metadata:
  name: gpu-pool
  namespace: default
spec:
  size: 3
  label: "pool=gpu-workloads"
  nodeSelector:
    nvidia.com/gpu.present: "true"
  taint: "gpu=true:NoSchedule"
```

```bash
# Apply the GPU pool
kubectl apply -f gpu-pool.yaml

# Check status
kubectl get nodepool gpu-pool -o wide
```

**What happens:**
- The operator ONLY selects nodes with label `nvidia.com/gpu.present=true`
- Applies label `pool=gpu-workloads` to selected nodes
- Applies taint `gpu=true:NoSchedule` to prevent non-GPU workloads from scheduling

**Schedule workloads on GPU pool:**

```yaml
# gpu-workload.yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-training-job
spec:
  nodeSelector:
    pool: gpu-workloads
  tolerations:
    - key: "gpu"
      operator: "Equal"
      value: "true"
      effect: "NoSchedule"
  containers:
    - name: training
      image: nvidia/cuda:latest
      resources:
        limits:
          nvidia.com/gpu: 1
```

---

### Scenario 3: Create Multiple Isolated Node Pools

Create separate pools for different teams or workloads.

```yaml
# team-pools.yaml
---
apiVersion: nodepool.k8s.local/v1
kind: NodePool
metadata:
  name: team-alpha
  namespace: default
spec:
  size: 2
  label: "team=alpha"
---
apiVersion: nodepool.k8s.local/v1
kind: NodePool
metadata:
  name: team-beta
  namespace: default
spec:
  size: 3
  label: "team=beta"
```

```bash
kubectl apply -f team-pools.yaml

# View all pools
kubectl get nodepools
```

**Expected output:**
```
NAME         PHASE   CURRENT   DESIRED   MESSAGE
team-alpha   Ready   2         2         Pool is ready (2/2 nodes available)
team-beta    Ready   3         3         Pool is ready (3/3 nodes available)
```

---

### Scenario 4: Scale Up a Node Pool

Increase the pool size to add more nodes.

```bash
# Edit the pool to increase size from 3 to 5
kubectl patch nodepool general-pool --type='merge' -p '{"spec":{"size":5}}'

# Or use kubectl edit
kubectl edit nodepool general-pool
```

**What happens:**
- The operator detects the change (desiredSize > currentSize)
- Finds 2 more eligible unassigned nodes
- Assigns them to the pool
- Updates status to reflect the new state
- Emits a `ScaledUp` event

**Watch the operation:**
```bash
kubectl describe nodepool general-pool
kubectl get events --field-selector involvedObject.name=general-pool
```

---

### Scenario 5: Scale Down a Node Pool (Safe Scale-Down)

The operator uses a **safe scale-down policy** — it will only release nodes that have been manually cordoned.

**Step 1: Reduce the desired size**
```bash
kubectl patch nodepool general-pool --type='merge' -p '{"spec":{"size":3}}'
```

**Step 2: Check pool status**
```bash
kubectl get nodepool general-pool
```

Output shows pending state:
```
NAME           PHASE     CURRENT   DESIRED   MESSAGE
general-pool   Pending   5         3         Over capacity (5/3), waiting for nodes to be cordoned
```

**Step 3: Cordon excess nodes**
```bash
# Identify nodes to remove
kubectl get nodes -l nodepool.k8s.local/name=general-pool

# Cordon the nodes you want to remove (drain workloads first if needed)
kubectl drain worker-node-4 --ignore-daemonsets --delete-emptydir-data
kubectl drain worker-node-5 --ignore-daemonsets --delete-emptydir-data
```

**Step 4: Operator automatically releases cordoned nodes**

The operator detects cordoned nodes and:
- Removes the pool label (`workload-type=general`)
- Removes the pool taint
- Removes the ownership label
- Updates the status

```bash
kubectl get nodepool general-pool
```

Output:
```
NAME           PHASE   CURRENT   DESIRED   MESSAGE
general-pool   Ready   3         3         Pool is ready (3/3 nodes available)
```

---

### Scenario 6: Node Maintenance

Mark a node for maintenance without removing it from the pool.

```bash
# Add maintenance label
kubectl label node worker-node-1 nodepool.k8s.local/maintenance=true

# The node is now considered "safe to release" but remains assigned
# The pool status will show degraded until maintenance is complete

kubectl get nodepool general-pool
```

Output:
```
NAME           PHASE      CURRENT   DESIRED   MESSAGE
general-pool   Degraded   2         3         Pool degraded: 2/3 nodes available (1 in maintenance)
```

**Remove from maintenance:**
```bash
kubectl label node worker-node-1 nodepool.k8s.local/maintenance-

# Pool returns to Ready state
```

---

### Scenario 7: Delete a Node Pool

When you delete a NodePool, the operator automatically cleans up all labels and taints from assigned nodes.

```bash
# Delete the pool
kubectl delete nodepool general-pool

# Watch the cleanup
kubectl get events -w
```

**What happens:**
1. Deletion is blocked by the finalizer
2. Operator releases all assigned nodes (removes labels and taints)
3. Finalizer is removed
4. NodePool is deleted

**Verify nodes are cleaned up:**
```bash
kubectl get nodes -l workload-type=general
# No nodes should be returned
```

---

## Monitoring and Observability

### View Pool Status

```bash
# List all pools with status
kubectl get nodepools -A

# Detailed status for a specific pool
kubectl describe nodepool <pool-name>

# JSON output for automation
kubectl get nodepool <pool-name> -o jsonpath='{.status}'
```

### View Events

```bash
# Events for a specific pool
kubectl get events --field-selector involvedObject.name=<pool-name>

# All nodepool-related events
kubectl get events | grep -i nodepool
```

### Controller Logs

```bash
# Stream controller logs
kubectl logs -n nodepool-operator-system deployment/nodepool-operator-controller-manager -f

# Filter for specific pool
kubectl logs -n nodepool-operator-system deployment/nodepool-operator-controller-manager | grep "nodepool=<pool-name>"
```

### Node Labels Check

```bash
# Find all nodes assigned to pools
kubectl get nodes -l nodepool.k8s.local/name

# Find nodes in a specific pool
kubectl get nodes -l nodepool.k8s.local/name=<pool-name>

# Show all labels on pool nodes
kubectl get nodes -l nodepool.k8s.local/name=<pool-name> --show-labels
```

---

## Troubleshooting

### Pool Stuck in "Pending" State

**Symptom:** Pool status shows `Pending` and `currentSize < desiredSize`

**Causes and Solutions:**

| Cause | Solution |
|-------|----------|
| Not enough eligible nodes | Add more worker nodes to the cluster |
| All nodes already assigned | Check if other NodePools have claimed available nodes |
| NodeSelector too restrictive | Verify nodes have required labels (`kubectl get nodes --show-labels`) |
| Nodes are cordoned | Uncordon nodes with `kubectl uncordon <node>` |

```bash
# Check eligible nodes count in logs
kubectl logs -n nodepool-operator-system deployment/nodepool-operator-controller-manager | grep "eligibleUnassigned"
```

### Pool Stuck in "Degraded" State

**Symptom:** Pool shows `Degraded` phase

**Causes:**
- Assigned nodes have become unschedulable (cordoned)
- Nodes have been put in maintenance mode
- Node is not Ready

```bash
# Check node conditions
kubectl get nodes -l nodepool.k8s.local/name=<pool-name> -o wide
kubectl describe node <problematic-node>
```

### Nodes Not Being Assigned

**Checklist:**
1. Verify node is Ready: `kubectl get nodes`
2. Verify node is not cordoned: `kubectl get nodes -o jsonpath='{.items[*].spec.unschedulable}'`
3. Verify node is not a control-plane node
4. Verify node is not already assigned to another pool
5. Verify nodeSelector matches node labels

---

## Uninstallation

### Remove All NodePools First

```bash
# Delete all NodePool resources (this triggers cleanup)
kubectl delete nodepools --all -A

# Wait for finalizers to complete
kubectl get nodepools -A
```

### Uninstall the Operator

```bash
# Remove the controller deployment
make undeploy

# Remove the CRDs
make uninstall
```

### Verify Cleanup

```bash
# Ensure no orphaned labels remain on nodes
kubectl get nodes -l nodepool.k8s.local/name
# Should return "No resources found"
```

---

## API Reference

### NodePoolSpec

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `size` | int32 | Yes | - | Desired number of nodes in the pool (≥0) |
| `label` | string | Yes | - | Label to apply, format: `key=value` |
| `nodeSelector` | map[string]string | No | - | Label selector to filter eligible nodes |
| `taint` | string | No | `<label-key>=<label-value>:NoSchedule` | Taint to apply, format: `key=value:effect` |

### NodePoolStatus

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | `Ready`, `Pending`, or `Degraded` |
| `currentSize` | int32 | Number of usable assigned nodes |
| `desiredSize` | int32 | Target node count from spec |
| `assignedNodes` | []string | List of assigned node names |
| `message` | string | Human-readable status message |

### Taint Effects

| Effect | Behavior |
|--------|----------|
| `NoSchedule` | Pods without matching toleration won't be scheduled |
| `PreferNoSchedule` | Scheduler avoids but may schedule if necessary |
| `NoExecute` | Evicts existing pods without toleration |

---

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.
