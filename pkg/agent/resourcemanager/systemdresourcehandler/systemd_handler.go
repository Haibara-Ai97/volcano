package systemdresourcehandler

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

type SystemdResourceHandler struct {
	conn          *dbus.Conn
	cgroupManager cgroup.CgroupManager
}

func NewSystemdResourceHandler() *SystemdResourceHandler {
	return &SystemdResourceHandler{}
}

func (srh *SystemdResourceHandler) SetCPUQoSLevel(ctx context.Context, podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error {
	serviceName := srh.getServiceName(podUID, qosClass)

	return srh.setQoSLevelViaDBus(serviceName, qosLevel)
}

func (srh *SystemdResourceHandler) getServiceName(podUID types.UID, qosClass corev1.PodQOSClass) string {
	cgroupPath, err := srh.cgroupManager.GetPodCgroupPath(qosClass, cgroup.CgroupCpuSubsystem, podUID)
	if err != nil {
		return ""
	}

	parts := strings.Split(cgroupPath, "/")
	for _, part := range parts {
		if strings.HasSuffix(part, ".service") {
			return part
		}
	}

	return ""
}

func (srh *SystemdResourceHandler) setQoSLevelViaDBus(serviceName string, qosLevel int64) error {
	obj := srh.conn.Object("org.freedesktop.systemd1", dbus.ObjectPath("/org/freedesktop/systemd1"))

	cpuWeight := utils.CalculateCPUWeightFromQoSLevel(qosLevel)
	cpuWeightVariant := dbus.MakeVariant(cpuWeight)

	call := obj.Call("org.freedesktop.systemd1.Manager.SetUnitProperties", 0,
		serviceName, false, []interface{}{
			"CPUWeight", cpuWeightVariant,
		})
	if call.Err != nil {
		return fmt.Errorf("Failed to set uint properties via D-Bus: %v", call.Err)
	}

	return nil
}
