CPU Throttle Feature Design
===

Design Principles
---

The CPU Throttle feature is a new addition to Volcano Agent that dynamically adjusts BestEffort (BE) CPU quotas based on node-level capacity for offline workloads. It follows a "probe-event-handler" architecture pattern, implementing periodic quota calculation and enforcement at the BE QoS cgroup level.

Core Design

1. **Quota Budgeting**: Enforces a BE CPU budget derived from node capacity and online workload demand
2. **Online-First**: Reserves capacity for online workloads before allocating the remaining CPU budget to BE pods
3. **QoS Awareness**: Only throttles low-priority BE workloads while protecting critical workloads

Implementation Architecture
---

### Probe Layer (nodemonitor)

**Responsibility**: Calculate the available BE CPU budget and emit throttling events

**Key Components**:

- `detectCPUThrottling()`: Core detection logic

- Configuration parameters: `cpuThrottlingThreshold` (capacity budget percentage)

**Quota Calculation**:

```
allowedBECPU = allocatableCPU * cpuThrottlingThreshold%
availableBECPU = max(allowedBECPU - onlinePodCPURequests, -1)
```


### Event Layer

Event Type: `NodeCPUThrottleEvent`

```go
type NodeCPUThrottleEvent struct {
    TimeStamp time.Time
    Resource  v1.ResourceName
    CPUQuotaMilli int64 // Available BE CPU quota in milli-CPU
}
```

### Handler Layer (cputhrottle)

**Responsibility**: Apply the BE CPU quota to the BE QoS root cgroup

**Core Algorithm**:

- **Quota Enforcement**: Applies the computed BE quota directly to the BestEffort cgroup
- **cgroup Operations**: Directly manipulates BE QoS cgroup quota files

**Key Methods**:

```go
quotaFromMilliCPU()
writeBEQuota()
```

Technical Implementation Details
---

**cgroup File Operations**

- BE cgroup path (systemd example): `/sys/fs/cgroup/cpu/kubepods.slice/kubepods-besteffort.slice`
- Write: Update quota value to BE root cgroup file (v1/v2 aware)
