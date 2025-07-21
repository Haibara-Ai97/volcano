package systemdhandler

import (
	"fmt"
	"github.com/godbus/dbus/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"strings"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

func (s *SystemdHandler) sendDBusToSystemd(serviceName string, properties []interface{}) error {
	if s.conn == nil {
		return fmt.Errorf("D-Bus connection is not available, cannot send D-Bus message")
	}

	obj := s.conn.Object("org.freedesktop.systemd1", dbus.ObjectPath("/org/freedesktop/systemd1"))

	call := obj.Call("org.freedesktop.systemd1.Manager.SetUnitProperties", 0,
		serviceName, false, properties)
	if call.Err != nil {
		return fmt.Errorf("failed to send D-Bus message: %v", call.Err)
	}
	return nil
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
