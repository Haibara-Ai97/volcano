package systemdhandler

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"volcano.sh/volcano/pkg/agent/resourcemanager/utils"
)

func (s *SystemdHandler) SetMemoryQos(podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error {
	if s.conn != nil {
		serviceName := s.getServiceName(podUID, qosClass)
		if serviceName != "" {
			return s.setMemoryQoSViaDBus(serviceName, qosLevel)
		}
	}

	return s.CgroupHandler.SetMemoryQoS(podUID, qosClass, qosLevel)
}

func (s *SystemdHandler) setMemoryQoSViaDBus(serviceName string, qosLevel int64) error {
	if s.conn == nil {
		return fmt.Errorf("D-Bus connection not available")
	}

	// 计算内存值
	memoryHigh := utils.CalculateMemoryHighFromQoSLevel(qosLevel)
	memoryLow := utils.CalculateMemoryLowFromQoSLevel(qosLevel)
	memoryMin := utils.CalculateMemoryMinFromQoSLevel(qosLevel)

	var properties []interface{}

	// 设置MemoryHigh (软限制)
	if memoryHigh > 0 {
		properties = append(properties, "MemoryHigh", memoryHigh)
	} else {
		properties = append(properties, "MemoryHigh", uint64(18446744073709551615))
	}

	if memoryLow > 0 {
		properties = append(properties, "MemoryLow", memoryLow)
	}

	if memoryMin > 0 {
		properties = append(properties, "MemoryMin", memoryMin)
	}

	err := s.sendDBusToSystemd(serviceName, properties)
	if err != nil {
		return fmt.Errorf("failed to set memory QoS via D-Bus: %v", err)
	}

	return nil
}
