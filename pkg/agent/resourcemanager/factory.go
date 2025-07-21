package resourcemanager

import (
	"volcano.sh/volcano/pkg/agent/resourcemanager/cgrouphandler"
	"volcano.sh/volcano/pkg/agent/resourcemanager/systemdhandler"
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
		return cgrouphandler.NewCgroupHandler(rhf.cgroupManager, rhf.cgroupVersion)
	case "systemd":
		return systemdhandler.NewSystemdHandler(rhf.cgroupManager, rhf.cgroupVersion)
	}
	return nil
}
