package resourcemanager

import (
	"volcano.sh/volcano/pkg/agent/resourcemanager/cgroupresourcehandler"
	"volcano.sh/volcano/pkg/agent/resourcemanager/systemdresourcehandler"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

type ResourceHandlerFactory struct {
	cgroupManager cgroup.CgroupManager
	cgroupVersion string
}

func NewResourceManagerFactory(cgroupMgr cgroup.CgroupManager, cgroupVersion string) *ResourceHandlerFactory {
	return &ResourceHandlerFactory{
		cgroupManager: cgroupMgr,
		cgroupVersion: cgroupVersion,
	}
}

func (rhf *ResourceHandlerFactory) CreateResourceHandler(cgroupDriver string) ResourceHandler {
	switch cgroupDriver {
	case "cgroupfs":
		return cgroupresourcehandler.NewCgroupResourceHandler(rhf.cgroupManager, rhf.cgroupVersion)
	case "systemd":
		return systemdresourcehandler.NewSystemdResourceHandler()
	}
	return nil
}
