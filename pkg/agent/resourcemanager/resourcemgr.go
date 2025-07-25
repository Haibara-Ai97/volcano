package resourcemanager

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sync"
	"volcano.sh/volcano/pkg/agent/events/framework"
	_ "volcano.sh/volcano/pkg/agent/resourcemanager/cgrouphandler"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

// ResourceManger 资源管理器
type ResourceManager struct {
	cgroupVersion string
	cgroupDriver  string
	Handler       ResourceHandler
	cgroupManger  cgroup.CgroupManager
	mu            sync.Mutex
}

// ResourceHandler 资源处理器接口
type ResourceHandler interface {
	SetCPUQoSLevel(podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error
	SetCPUBurst(qosClass corev1.PodQOSClass, podUID types.UID, quotaBurstTime int64, pod *corev1.Pod) error
	SetMemoryQoS(podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error
	SetResourceLimit(podEvent framework.PodEvent) error
}

func NewResourceManager(cgroupVersion, cgroupDriver string, cgroupManger cgroup.CgroupManager) *ResourceManager {
	factory := NewResourceManagerFactory(cgroupManger, cgroupVersion)
	handler := factory.CreateResourceHandler(cgroupDriver)

	return &ResourceManager{
		cgroupVersion: cgroupVersion,
		cgroupDriver:  cgroupDriver,
		Handler:       handler,
		cgroupManger:  cgroupManger,
	}
}
