package systemdhandler

import (
	"fmt"

	"github.com/godbus/dbus/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"volcano.sh/volcano/pkg/agent/resourcemanager/utils"
)

func (s *SystemdHandler) SetCPUQoSLevel(podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error {
	serviceName := s.getServiceName(podUID, qosClass)
	if serviceName == "" {
		return fmt.Errorf("failed to get service name for pod %s", podUID)
	}

	return s.setQoSLevelViaDBus(serviceName, qosLevel)
}

func (s *SystemdHandler) setQoSLevelViaDBus(serviceName string, qosLevel int64) error {
	if s.conn == nil {
		return fmt.Errorf("D-Bus connection not available, cannot set QoS level via systemd")
	}

	var cpuWeight uint64

	if qosLevel == -1 {
		// For idle level, use special value 0 which will be treated as "idle"
		cpuWeight = 0
	} else {
		cpuWeight = uint64(utils.CalculateCPUWeightFromQoSLevel(qosLevel))
	}

	// Set CPUWeight - this is sufficient for QoS level control
	cpuWeightProperties := []interface{}{
		"CPUWeight", dbus.MakeVariant(cpuWeight),
	}
	if err := s.sendDBusToSystemd(serviceName, cpuWeightProperties); err != nil {
		return fmt.Errorf("failed to set CPUWeight via D-Bus: %v", err)
	}

	// todo: Try to fallback to cgroup handler
	// Try to set CPU quota if supported by this systemd version
	// CPUWeight alone is sufficient for basic QoS control
	if err := s.trySetCPUQuota(serviceName, qosLevel); err != nil {
		fmt.Printf("Warning: Failed to set CPU quota (will use CPUWeight only): %v\n", err)
	}

	return nil
}

// trySetCPUQuota attempts to set CPU quota via D-Bus
// If it fails (e.g., unsupported by this systemd version), it returns an error
// but doesn't fail the entire QoS operation
func (s *SystemdHandler) trySetCPUQuota(serviceName string, qosLevel int64) error {
	if s.conn == nil {
		return fmt.Errorf("D-Bus connection not available")
	}

	// Calculate CPU quota in microseconds per second
	cpuQuota := utils.CalculateCPUQuotaFromQoSLevel(qosLevel)
	var cpuQuotaUSec uint64

	if qosLevel == -1 {
		// For idle level, set to infinity (0 means infinity in systemd)
		cpuQuotaUSec = 0
	} else if cpuQuota > 0 {
		// Convert percentage to microseconds per second
		// 100% = 1000000 microseconds per second
		// cpuQuota is in percentage (e.g., 10 means 10%)
		// For systemd, we need to set a reasonable value within the valid range
		if cpuQuota >= 100 {
			cpuQuotaUSec = 1000000 // 100% = 1000000 microseconds
		} else {
			cpuQuotaUSec = uint64(cpuQuota * 10000) // Convert percentage to microseconds
		}
	} else {
		cpuQuotaUSec = 0 // 0 means infinity
	}

	// Try different CPU quota property names that might be supported
	quotaProperties := []string{
		"CPUQuotaPerSecUSec", // Modern systemd
		"CPUQuota",           // Older systemd versions
	}

	for _, propertyName := range quotaProperties {
		cpuQuotaProperties := []interface{}{
			propertyName, dbus.MakeVariant(cpuQuotaUSec),
		}

		if err := s.sendDBusToSystemd(serviceName, cpuQuotaProperties); err == nil {
			// Successfully set the quota
			return nil
		}
		// If this property name failed, try the next one
	}

	return fmt.Errorf("failed to set CPU quota with any supported property name")
}
