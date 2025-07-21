package systemdhandler

import (
	"fmt"

	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/agent/events/framework"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
)

func (s *SystemdHandler) SetResourceLimit(podEvent framework.PodEvent) error {
	resources := utilpod.CalculateExtendResourceSystemd(podEvent.Pod)
	// 合并同一个 serviceName 的属性，批量 D-Bus 调用
	serviceProps := make(map[string][]interface{})
	for _, resource := range resources {
		serviceName := s.getServiceName(podEvent.UID, podEvent.QoSClass)
		if serviceName == "" {
			klog.ErrorS(nil, "Failed to get systemd service name", "podUID", podEvent.UID, "qosClass", podEvent.QoSClass)
			continue
		}
		// systemd D-Bus属性格式: [property, dbus.MakeVariant(value)]
		serviceProps[serviceName] = append(serviceProps[serviceName], resource.Property, resource.Value)
	}

	var errs []error
	for serviceName, props := range serviceProps {
		if len(props) == 0 {
			continue
		}
		err := s.sendDBusToSystemd(serviceName, props)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to set systemd resource for %s: %w", serviceName, err))
			klog.ErrorS(err, "Failed to set systemd resource", "serviceName", serviceName, "properties", props)
		} else {
			klog.InfoS("Successfully set systemd resource", "serviceName", serviceName, "properties", props)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("SetResourceLimit encountered errors: %v", errs)
	}
	return nil
}
