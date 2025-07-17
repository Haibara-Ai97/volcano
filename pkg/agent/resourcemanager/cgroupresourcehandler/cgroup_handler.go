package cgroupresourcehandler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	calutils "volcano.sh/volcano/pkg/agent/resourcemanager/utils"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

type CgroupResourceHandler struct {
	cgroupVersion string
	cgroupMgr     cgroup.CgroupManager
}

func NewCgroupResourceHandler(cgroupMgr cgroup.CgroupManager, cgroupVersion string) *CgroupResourceHandler {
	return &CgroupResourceHandler{
		cgroupMgr:     cgroupMgr,
		cgroupVersion: cgroupVersion,
	}
}

func (crh *CgroupResourceHandler) SetCPUQoSLevel(ctx context.Context, podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error {
	// todo: adapt cgroup v2 without .qos_level
	cgroupPath, err := crh.cgroupMgr.GetPodCgroupPath(qosClass, cgroup.CgroupCpuSubsystem, podUID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup file(%s), error: %v", podUID, err)
	}
	switch crh.cgroupVersion {
	case "cgroupv1":
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
	case "cgroupv2":
		return crh.setCPUWeightAndQuota(cgroupPath, qosLevel)
	default:
		return fmt.Errorf("invalid cgroup version: %s", crh.cgroupVersion)
	}
}

func (crh *CgroupResourceHandler) setCPUWeightAndQuota(cgroupPath string, qosLevel int64) error {
	cpuWeight := calutils.CalculateCPUWeightFromQoSLevel(qosLevel)

	cpuWeightFile := path.Join(cgroupPath, cgroup.CPUWeightFileV2)
	cpuWeightByte := []byte(fmt.Sprintf("%d", cpuWeight))

	err := utils.UpdatePodCgroup(cpuWeightFile, cpuWeightByte)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.InfoS("Cgroup cpu weight file not exist", "cgroupFile", cpuWeightFile)
		} else {
			return err
		}
	}

	if cpuQuota := calutils.CalculateCPUQuotaFromQoSLevel(qosLevel); cpuQuota > 0 {
		cpuMaxFile := path.Join(cgroupPath, cgroup.CPUQuotaTotalFileV2)
		cpuMaxByte := []byte(fmt.Sprintf("%d", cpuQuota))

		err = utils.UpdatePodCgroup(cpuMaxFile, cpuMaxByte)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				klog.InfoS("Cgroup cpu quota file not exist", "cgroupFile", cpuMaxFile)
			} else {
				return err
			}
		}
	}
	return nil
}
