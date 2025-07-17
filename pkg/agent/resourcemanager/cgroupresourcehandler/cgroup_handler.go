package cgroupresourcehandler

import (
	"context"
	"errors"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"os"
	"path"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

type CgroupResourceHandler struct {
	cgroupMgr cgroup.CgroupManager
}

func NewCgroupResourceHandler(cgroupMgr cgroup.CgroupManager) *CgroupResourceHandler {
	return &CgroupResourceHandler{
		cgroupMgr: cgroupMgr,
	}
}

func (crh *CgroupResourceHandler) SetCPUQoSLevel(ctx context.Context, podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error {
	// todo: adapt cgroup v2 without .qos_level
	cgroupPath, err := crh.cgroupMgr.GetPodCgroupPath(qosClass, cgroup.CgroupCpuSubsystem, podUID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup file(%s), error: %v", podUID, err)
	}

	qosLevelFile := path.Join(cgroupPath, cgroup.CPUQoSLevelFile)
	qosLevelByte := []byte(fmt.Sprintf("%d", qosLevel))

	err = utils.UpdatePodCgroup(qosLevelFile, qosLevelByte)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.InfoS("Cgroup file not exist", "cgroupFile", qosLevelFile)
			return nil
		}
		return err
	}
	return nil
}
