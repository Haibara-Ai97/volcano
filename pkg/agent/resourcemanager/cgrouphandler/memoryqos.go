package cgrouphandler

import (
	"errors"
	"fmt"
	"os"
	"path"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/agent/apis/extension"
	calutils "volcano.sh/volcano/pkg/agent/resourcemanager/utils"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

func (c *CgroupHandler) SetMemoryQoS(podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64) error {
	cgroupPath, err := c.cgroupMgr.GetPodCgroupPath(qosClass, cgroup.CgroupMemorySubsystem, podUID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup file(%s), error: %v", podUID, err)
	}

	switch c.cgroupVersion {
	case "cgroupv1":
		qosLevelFile := path.Join(cgroupPath, cgroup.MemoryQoSLevelFile)
		qosLevelInt64 := []byte(fmt.Sprintf("%d", extension.NormalizeQosLevel(qosLevel)))

		err = utils.UpdatePodCgroup(qosLevelFile, qosLevelInt64)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				klog.InfoS("Cgroup file not existed", "cgroupFile", qosLevelFile)
				return nil
			}
			return err
		}
		return nil
	case "cgroupv2":
		return c.setMemoryQoSV2(cgroupPath, qosLevel)
	default:
		return fmt.Errorf("invalid cgroup version: %s", c.cgroupVersion)
	}
}

func (c *CgroupHandler) setMemoryQoSV2(cgroupPath string, qosLevel int64) error {
	// Set memory.high (soft limit)
	memoryHigh := calutils.CalculateMemoryHighFromQoSLevel(qosLevel)
	memoryHighFile := path.Join(cgroupPath, cgroup.MemoryHighFileV2)
	var memoryHighByte []byte
	if memoryHigh == 0 {
		memoryHighByte = []byte("max") // cgroup v2 uses "max" for no limit
	} else {
		memoryHighByte = []byte(fmt.Sprintf("%d", memoryHigh))
	}

	err := utils.UpdatePodCgroup(memoryHighFile, memoryHighByte)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.InfoS("Cgroup memory high file not exist", "cgroupFile", memoryHighFile)
		} else {
			return err
		}
	}

	// Set memory.low (minimum guarantee)
	memoryLow := calutils.CalculateMemoryLowFromQoSLevel(qosLevel)
	memoryLowFile := path.Join(cgroupPath, cgroup.MemoryLowFileV2)
	memoryLowByte := []byte(fmt.Sprintf("%d", memoryLow))

	err = utils.UpdatePodCgroup(memoryLowFile, memoryLowByte)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.InfoS("Cgroup memory low file not exist", "cgroupFile", memoryLowFile)
		} else {
			return err
		}
	}

	// Set memory.min (minimum reservation)
	memoryMin := calutils.CalculateMemoryMinFromQoSLevel(qosLevel)
	memoryMinFile := path.Join(cgroupPath, cgroup.MemoryMinFileV2)
	memoryMinByte := []byte(fmt.Sprintf("%d", memoryMin))

	err = utils.UpdatePodCgroup(memoryMinFile, memoryMinByte)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.InfoS("Cgroup memory min file not exist", "cgroupFile", memoryMinFile)
		} else {
			return err
		}
	}

	klog.InfoS("Successfully set memory QoS for cgroup v2", "qosLevel", qosLevel, "memoryHigh", string(memoryHighByte), "memoryLow", memoryLow, "memoryMin", memoryMin)
	return nil
}
