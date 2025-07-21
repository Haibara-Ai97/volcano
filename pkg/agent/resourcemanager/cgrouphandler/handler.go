package cgrouphandler

import (
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

type CgroupHandler struct {
	cgroupVersion string
	cgroupMgr     cgroup.CgroupManager
}

func NewCgroupHandler(cgroupMgr cgroup.CgroupManager, cgroupVersion string) *CgroupHandler {
	return &CgroupHandler{
		cgroupMgr:     cgroupMgr,
		cgroupVersion: cgroupVersion,
	}
}
