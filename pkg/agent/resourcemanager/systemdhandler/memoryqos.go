package systemdhandler

import (
	"fmt"
	"github.com/godbus/dbus/v5"
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

	// 创建DBus对象
	obj := s.conn.Object("org.freedesktop.systemd1", dbus.ObjectPath("/org/freedesktop/systemd1"))

	// 构建属性数组
	var properties []interface{}

	// 设置MemoryHigh (软限制)
	if memoryHigh > 0 {
		properties = append(properties, "MemoryHigh", dbus.MakeVariant(memoryHigh))
	} else {
		// 无限制时设置为uint64最大值
		properties = append(properties, "MemoryHigh", dbus.MakeVariant(uint64(18446744073709551615)))
	}

	// 设置MemoryLow (最小保证)
	if memoryLow > 0 {
		properties = append(properties, "MemoryLow", dbus.MakeVariant(memoryLow))
	}

	// 设置MemoryMin (最小预留)
	if memoryMin > 0 {
		properties = append(properties, "MemoryMin", dbus.MakeVariant(memoryMin))
	}

	// 调用DBus方法
	call := obj.Call("org.freedesktop.systemd1.Manager.SetUnitProperties", 0,
		serviceName, false, properties)

	if call.Err != nil {
		return fmt.Errorf("failed to set memory QoS via D-Bus: %v", call.Err)
	}

	return nil
}
