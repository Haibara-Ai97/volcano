package cputhrottle

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sync"
	"volcano.sh/volcano/pkg/agent/apis/extension"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.NodeCPUThrottleEventName), NewCPUThrottleHandler)
}

type CPUThrottleHandler struct {
	*base.BaseHandle
	cgroupMgr   cgroup.CgroupManager
	getPodsFunc utilpod.ActivePods

	// Record Pod throttled status
	mutex            sync.RWMutex
	originalQuotas   map[string]int64
	currentQuotas    map[string]int64
	throttlingActive map[string]bool
}

func NewCPUThrottleHandler(config *config.Configuration, mgr *metriccollect.MetricCollectorManager,
	cgroupMgr cgroup.CgroupManager) framework.Handle {
	return &CPUThrottleHandler{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.CPUThrottleFeature),
			Config: config,
		},
		cgroupMgr:        cgroupMgr,
		getPodsFunc:      config.GetActivePods,
		originalQuotas:   make(map[string]int64),
		currentQuotas:    make(map[string]int64),
		throttlingActive: make(map[string]bool),
	}
}

func (h *CPUThrottleHandler) Handle(event interface{}) error {
	cpuEvent, ok := event.(framework.NodeCPUThrottleEvent)
	if !ok {
		return fmt.Errorf("invalid event type for CPU Throttle handler")
	}

	if cpuEvent.Resource != v1.ResourceCPU {
		return nil
	}

	pods, err := h.getPodsFunc()
	if err != nil {
		return fmt.Errorf("failed to get active pods: %v", err)
	}

	klog.InfoS("Handling CPU throttling event",
		"action", cpuEvent.Action,
		"usage", cpuEvent.Usage,
		"podCount", len(pods))

	switch cpuEvent.Action {
	case "start":
		return h.stepThrottleCPU(pods)
	case "stop":
		return h.stopCPUThrottle(pods)
	default:
		return fmt.Errorf("unknown cpu throttle action: %v", cpuEvent.Action)
	}
}

func (h *CPUThrottleHandler) stepThrottleCPU(pods []*v1.Pod) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	for _, pod := range pods {
		qosLevel := extension.GetQosLevel(pod)
		if qosLevel >= 0 {
			continue
		}

		podUID := string(pod.UID)

		currentQuota, err := h.getCurrentCPUQuota(pod)
		if err != nil {
			klog.ErrorS(err, "Failed to get current CPU quota", "pod", pod.Name)
			continue
		}

		// If the first time this pod is throttled, record the original cpu quota for recover lately
		if _, exists := h.originalQuotas[podUID]; !exists {
			h.originalQuotas[podUID] = currentQuota
			h.currentQuotas[podUID] = currentQuota
		}

		newQuota := h.calculateSteppedQuota(pod, currentQuota)

		if newQuota == currentQuota {
			continue
		}

		// Apply the calculated cpu quota for this pod
		if err := h.applyCPUQuota(pod, newQuota); err != nil {
			klog.ErrorS(err, "Failed to apply CPU quota", "pod", pod.Name, "quota", newQuota)
			continue
		}

		h.currentQuotas[podUID] = newQuota
		h.throttlingActive[podUID] = true

		klog.InfoS("Applied stepped CPU throttling",
			"pod", pod.Name,
			"originalQuota", h.originalQuotas[podUID],
			"currentQuota", currentQuota,
			"newQuota", newQuota)
	}

	return nil
}

func (h *CPUThrottleHandler) stopCPUThrottle(pods []*v1.Pod) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	for _, pod := range pods {
		qosLevel := extension.GetQosLevel(pod)
		if qosLevel >= 0 {
			continue
		}

		podUID := string(pod.UID)

		if !h.throttlingActive[podUID] {
			continue
		}

		originalQuota, exists := h.originalQuotas[podUID]
		if !exists {
			continue
		}

		currentQuota := h.currentQuotas[podUID]
		newQuota := h.calculateRecoveredQuota(currentQuota, originalQuota)

		if err := h.applyCPUQuota(pod, newQuota); err != nil {
			klog.ErrorS(err, "Failed to recover CPU quota", "pod", pod.Name)
			continue
		}

		h.currentQuotas[podUID] = newQuota

		// If cpu throttle has been fully restored, clear the record in maps
		if newQuota >= originalQuota {
			delete(h.originalQuotas, podUID)
			delete(h.currentQuotas, podUID)
			delete(h.throttlingActive, podUID)
		}

		klog.InfoS("Recovered CPU throttling",
			"pod", pod.Name,
			"currentQuota", currentQuota,
			"newQuota", newQuota,
			"originalQuota", originalQuota)
	}

	return nil
}
