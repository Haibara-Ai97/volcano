package systemdhandler

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"volcano.sh/volcano/pkg/agent/resourcemanager/utils"
)

func (s *SystemdHandler) SetCPUQoSLevel(ctx context.Context, podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error {
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

	cpuWeight := utils.CalculateCPUWeightFromQoSLevel(qosLevel)

	// Create properties array: ["CPUWeight", dbus.MakeVariant(cpuWeight)]
	properties := []interface{}{
		"CPUWeight", cpuWeight,
	}

	err := s.sendDBusToSystemd(serviceName, properties)
	if err != nil {
		return fmt.Errorf("failed to set CPUWeight via D-Bus: %v", err)
	}

	return nil
}
