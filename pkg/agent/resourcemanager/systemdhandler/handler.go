package systemdhandler

import (
	"volcano.sh/volcano/pkg/agent/resourcemanager/cgrouphandler"

	"github.com/godbus/dbus/v5"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

type SystemdHandler struct {
	*cgrouphandler.CgroupHandler
	conn          *dbus.Conn
	cgroupManager cgroup.CgroupManager
	cgroupVersion string
}

func NewSystemdHandler(cgroupMgr cgroup.CgroupManager, cgroupVersion string) *SystemdHandler {
	// Initialize D-Bus connection
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		// Log the error but don't fail initialization - the handler can still work for cgroupfs operations
		// D-Bus operations will fail gracefully if needed
		return &SystemdHandler{
			CgroupHandler: cgrouphandler.NewCgroupHandler(cgroupMgr, cgroupVersion),
			cgroupManager: cgroupMgr,
			cgroupVersion: cgroupVersion,
			conn:          nil, // Will be nil if connection failed
		}
	}

	return &SystemdHandler{
		CgroupHandler: cgrouphandler.NewCgroupHandler(cgroupMgr, cgroupVersion),
		cgroupManager: cgroupMgr,
		cgroupVersion: cgroupVersion,
		conn:          conn,
	}
}
