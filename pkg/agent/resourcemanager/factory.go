package resourcemanager

import (
	"volcano.sh/volcano/pkg/agent/resourcemanager/cgroupresourcehandler"
	"volcano.sh/volcano/pkg/agent/resourcemanager/systemdresourcehandler"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

type ResourceHandlerFactory struct {
	cgroupManager cgroup.CgroupManager
}

func NewResourceManagerFactory(cgroupMgr cgroup.CgroupManager) *ResourceHandlerFactory {
	return &ResourceHandlerFactory{
		cgroupManager: cgroupMgr,
	}
}

func (rhf *ResourceHandlerFactory) CreateResourceHandler(cgroupDriver string) ResourceHandler {
	switch cgroupDriver {
	case "cgroupfs":
		return cgroupresourcehandler.NewCgroupResourceHandler(rhf.cgroupManager)
	case "systemd":
		return systemdresourcehandler.NewSystemdResourceHandler()
	}
	return nil
}
