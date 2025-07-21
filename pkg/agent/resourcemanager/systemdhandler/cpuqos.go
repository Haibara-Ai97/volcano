package systemdhandler

import (
	"context"
	"fmt"
	"github.com/godbus/dbus/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"strings"
	"volcano.sh/volcano/pkg/agent/resourcemanager/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

func (s *SystemdHandler) SetCPUQoSLevel(ctx context.Context, podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error {
	serviceName := s.getServiceName(podUID, qosClass)
	if serviceName == "" {
		return fmt.Errorf("failed to get service name for pod %s", podUID)
	}

	return s.setQoSLevelViaDBus(serviceName, qosLevel)
}

func (s *SystemdHandler) getServiceName(podUID types.UID, qosClass corev1.PodQOSClass) string {
	cgroupPath, err := s.cgroupManager.GetPodCgroupPath(qosClass, cgroup.CgroupCpuSubsystem, podUID)
	if err != nil {
		return ""
	}

	parts := strings.Split(cgroupPath, "/")
	for _, part := range parts {
		// In systemd-managed cgroups (both v1 and v2), we look for .slice suffix
		if strings.HasSuffix(part, ".slice") {
			// Validate that this service/slice actually exists in systemd
			if s.validateServiceExists(part) {
				return part
			}
		}
	}

	return ""
}

// validateServiceExists checks if the service/slice exists in systemd
func (s *SystemdHandler) validateServiceExists(serviceName string) bool {
	if s.conn == nil {
		// If D-Bus connection is not available, we can't validate
		// Return true to allow the operation to proceed
		return true
	}

	obj := s.conn.Object("org.freedesktop.systemd1", dbus.ObjectPath("/org/freedesktop/systemd1"))

	// Try to get unit properties to check if the unit exists
	call := obj.Call("org.freedesktop.systemd1.Manager.GetUnit", 0, serviceName)
	if call.Err != nil {
		// Unit doesn't exist or other error
		return false
	}

	// If we get here, the unit exists
	return true
}

func (s *SystemdHandler) setQoSLevelViaDBus(serviceName string, qosLevel int64) error {
	if s.conn == nil {
		return fmt.Errorf("D-Bus connection not available, cannot set QoS level via systemd")
	}

	cpuWeight := utils.CalculateCPUWeightFromQoSLevel(qosLevel)

	// Use Manager.SetUnitProperties method to set CPUWeight
	// Format: SetUnitProperties(unit_name, runtime, properties)
	obj := s.conn.Object("org.freedesktop.systemd1", dbus.ObjectPath("/org/freedesktop/systemd1"))

	// Create properties array: [("CPUWeight", variant(500))]
	properties := []interface{}{
		"CPUWeight", dbus.MakeVariant(cpuWeight),
	}

	call := obj.Call("org.freedesktop.systemd1.Manager.SetUnitProperties", 0,
		serviceName, false, properties)
	if call.Err != nil {
		return fmt.Errorf("failed to set CPUWeight via D-Bus: %v", call.Err)
	}

	return nil
}
