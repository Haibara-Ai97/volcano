package systemdresourcehandler

import (
	"context"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"volcano.sh/volcano/pkg/agent/resourcemanager/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

type SystemdResourceHandler struct {
	conn          *dbus.Conn
	cgroupManager cgroup.CgroupManager
	cgroupVersion string
}

func NewSystemdResourceHandler(cgroupMgr cgroup.CgroupManager, cgroupVersion string) *SystemdResourceHandler {
	// Initialize D-Bus connection
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		// Log the error but don't fail initialization - the handler can still work for cgroupfs operations
		// D-Bus operations will fail gracefully if needed
		return &SystemdResourceHandler{
			cgroupManager: cgroupMgr,
			cgroupVersion: cgroupVersion,
			conn:          nil, // Will be nil if connection failed
		}
	}

	return &SystemdResourceHandler{
		cgroupManager: cgroupMgr,
		cgroupVersion: cgroupVersion,
		conn:          conn,
	}
}

func (srh *SystemdResourceHandler) SetCPUQoSLevel(ctx context.Context, podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error {
	serviceName := srh.getServiceName(podUID, qosClass)
	if serviceName == "" {
		return fmt.Errorf("failed to get service name for pod %s", podUID)
	}

	return srh.setQoSLevelViaDBus(serviceName, qosLevel)
}

func (srh *SystemdResourceHandler) getServiceName(podUID types.UID, qosClass corev1.PodQOSClass) string {
	cgroupPath, err := srh.cgroupManager.GetPodCgroupPath(qosClass, cgroup.CgroupCpuSubsystem, podUID)
	if err != nil {
		return ""
	}

	parts := strings.Split(cgroupPath, "/")
	for _, part := range parts {
		// In systemd-managed cgroups (both v1 and v2), we look for .slice suffix
		if strings.HasSuffix(part, ".slice") {
			// Validate that this service/slice actually exists in systemd
			if srh.validateServiceExists(part) {
				return part
			}
		}
	}

	return ""
}

// validateServiceExists checks if the service/slice exists in systemd
func (srh *SystemdResourceHandler) validateServiceExists(serviceName string) bool {
	if srh.conn == nil {
		// If D-Bus connection is not available, we can't validate
		// Return true to allow the operation to proceed
		return true
	}

	obj := srh.conn.Object("org.freedesktop.systemd1", dbus.ObjectPath("/org/freedesktop/systemd1"))
	
	// Try to get unit properties to check if the unit exists
	call := obj.Call("org.freedesktop.systemd1.Manager.GetUnit", 0, serviceName)
	if call.Err != nil {
		// Unit doesn't exist or other error
		return false
	}
	
	// If we get here, the unit exists
	return true
}

func (srh *SystemdResourceHandler) setQoSLevelViaDBus(serviceName string, qosLevel int64) error {
	if srh.conn == nil {
		return fmt.Errorf("D-Bus connection not available, cannot set QoS level via systemd")
	}

	cpuWeight := utils.CalculateCPUWeightFromQoSLevel(qosLevel)

	// Use Manager.SetUnitProperties method to set CPUWeight
	// Format: SetUnitProperties(unit_name, runtime, properties)
	obj := srh.conn.Object("org.freedesktop.systemd1", dbus.ObjectPath("/org/freedesktop/systemd1"))

	// Create properties array: [("CPUWeight", variant(500))]
	properties := []interface{}{
		"CPUWeight", dbus.MakeVariant(cpuWeight),
	}

	call := obj.Call("org.freedesktop.systemd1.Manager.SetUnitProperties", 0,
		serviceName, false, properties)
	if call.Err != nil {
		return fmt.Errorf("Failed to set CPUWeight via D-Bus: %v", call.Err)
	}

	return nil
}
