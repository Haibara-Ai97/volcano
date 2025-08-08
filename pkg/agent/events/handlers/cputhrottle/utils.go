package cputhrottle

import (
	"errors"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"os"
	"path"
	"strconv"
	"strings"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

const (
	// CPUPeriod CPU schedule period
	CPUPeriod = 100000 // 100ms in microseconds

	// ThrottleStepPercent step set cpu throttle
	ThrottleStepPercent = 10
	MinCPUQuotaPercent  = 20

	// RecoverStepPercent pod recover step
	RecoverStepPercent = 15
)

func (h *CPUThrottleHandler) getCurrentCPUQuota(pod *v1.Pod) (int64, error) {
	cgroupPath, err := h.cgroupMgr.GetPodCgroupPath(pod.Status.QOSClass, cgroup.CgroupCpuSubsystem, pod.UID)
	if err != nil {
		return 0, fmt.Errorf("failed to get pod cgroup path: %v", err)
	}

	quotaFile := path.Join(cgroupPath, cgroup.CPUQuotaTotalFile)

	data, err := os.ReadFile(quotaFile)
	if err != nil {
		if os.IsNotExist(err) {
			return h.getDefaultCPUQuota(pod), nil
		}
		return 0, fmt.Errorf("failed to read CPU quota file: %v", err)
	}

	quotaStr := strings.TrimSpace(string(data))
	if quotaStr == "-1" {
		return h.getDefaultCPUQuota(pod), nil
	}

	quota, err := strconv.ParseInt(quotaStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse CPU quota: %v", err)
	}

	return quota, nil
}

func (h *CPUThrottleHandler) getDefaultCPUQuota(pod *v1.Pod) int64 {
	// Calculate default cpu quota based on containers' cpu limits
	for _, container := range pod.Spec.Containers {
		if cpuLimit := container.Resources.Limits.Cpu(); cpuLimit != nil {
			cpuMillis := cpuLimit.MilliValue()
			quota := cpuMillis * CPUPeriod / 1000
			return quota
		}
	}

	// If cpu has no limit, return a large default
	return 2 * CPUPeriod
}

func (h *CPUThrottleHandler) calculateSteppedQuota(pod *v1.Pod, currentQuota int64) int64 {
	// Calculate the quota for the tiered limit
	stepReduction := currentQuota * ThrottleStepPercent / 100
	newQuota := currentQuota - stepReduction

	// Calculate the min quota based on protection watermark
	minQuota := h.calculateMinCPUQuota(pod)

	if newQuota < minQuota {
		return minQuota
	}

	return newQuota
}

func (h *CPUThrottleHandler) calculateMinCPUQuota(pod *v1.Pod) int64 {
	var minQuota int64

	for _, container := range pod.Spec.Containers {
		if cpuRequest := container.Resources.Requests.Cpu(); cpuRequest != nil {
			cpuMillis := cpuRequest.MilliValue()
			containerMinQuota := cpuMillis * CPUPeriod / 1000
			minQuota += containerMinQuota
		}
	}

	if minQuota == 0 {
		originalQuota := h.originalQuotas[string(pod.UID)]
		if originalQuota > 0 {
			minQuota = originalQuota * MinCPUQuotaPercent / 100
		} else {
			minQuota = CPUPeriod * MinCPUQuotaPercent / 100
		}
	}

	return minQuota
}

func (h *CPUThrottleHandler) calculateRecoveredQuota(currentQuota, originalQuota int64) int64 {
	stepIncrease := originalQuota * RecoverStepPercent / 100
	newQuota := currentQuota + stepIncrease

	if newQuota > originalQuota {
		return originalQuota
	}

	return newQuota
}

func (h *CPUThrottleHandler) applyCPUQuota(pod *v1.Pod, quota int64) error {
	cgroupPath, err := h.cgroupMgr.GetPodCgroupPath(pod.Status.QOSClass, cgroup.CgroupCpuSubsystem, pod.UID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup path: %v", err)
	}

	quotaFile := path.Join(cgroupPath, cgroup.CPUQuotaTotalFile)
	quotaByte := []byte(fmt.Sprintf("%d", quota))

	err = utils.UpdatePodCgroup(quotaFile, quotaByte)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.InfoS("Cgroup file not existed", "cgroupFile", quotaFile)
			return nil
		}
		return fmt.Errorf("failed to update CPU quota: %v", err)
	}

	periodFile := path.Join(cgroupPath, "cpu.cfs_period_us")
	periodByte := []byte(fmt.Sprintf("%d", CPUPeriod))

	if err := utils.UpdatePodCgroup(periodFile, periodByte); err != nil && !os.IsNotExist(err) {
		klog.ErrorS(err, "Failed to set CPU period", "pod", pod.Name)
	}

	return nil
}
