package resourcemanager

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sync"
	_ "volcano.sh/volcano/pkg/agent/resourcemanager/cgroupresourcehandler"
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
	SetCPUQoSLevel(ctx context.Context, podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error
}

func NewResourceManager(cgroupVersion, cgroupDriver string, cgroupManger cgroup.CgroupManager) *ResourceManager {
	factory := NewResourceManagerFactory(cgroupManger)
	handler := factory.CreateResourceHandler(cgroupDriver)

	return &ResourceManager{
		cgroupVersion: cgroupVersion,
		cgroupDriver:  cgroupDriver,
		Handler:       handler,
		cgroupManger:  cgroupManger,
	}
}
