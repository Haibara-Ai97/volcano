package cgrouphandler

import (
	"errors"
	"fmt"
	"io/fs"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"os"
	"path/filepath"
	"strconv"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/agent/utils/file"
)

func (c *CgroupHandler) SetCPUBurst(qosClass corev1.PodQOSClass, podUID types.UID, quotaBurstTime int64, pod *corev1.Pod) error {
	cgroupPath, err := c.cgroupMgr.GetPodCgroupPath(qosClass, cgroup.CgroupCpuSubsystem, podUID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup file(%s), error: %v", podUID, err)
	}

	podBurstTime := int64(0)
	err = filepath.WalkDir(cgroupPath, walkFunc(cgroupPath, quotaBurstTime, &podBurstTime, c.cgroupVersion))
	if err != nil {
		return fmt.Errorf("failed to set container cpu quota burst time, err: %v", err)
	}

	cgroupVersion := c.cgroupMgr.GetCgroupVersion()
	var podQuotaTotalFile, podQuotaBurstFile string
	if cgroupVersion == cgroup.CgroupV2 {
		podQuotaTotalFile = filepath.Join(cgroupPath, cgroup.CPUQuotaTotalFileV2)
		podQuotaBurstFile = filepath.Join(cgroupPath, cgroup.CPUQuotaBurstFileV2)
	} else {
		podQuotaTotalFile = filepath.Join(cgroupPath, cgroup.CPUQuotaTotalFile)
		podQuotaBurstFile = filepath.Join(cgroupPath, cgroup.CPUQuotaBurstFile)
	}

	value, err := file.ReadIntFromFile(podQuotaTotalFile)
	if err != nil {
		return fmt.Errorf("failed to get pod cpu total quota time, err: %v,path: %s", err, podQuotaTotalFile)
	}
	if value == fixedQuotaValue {
		return nil
	}
	err = utils.UpdateFile(podQuotaBurstFile, []byte(strconv.FormatInt(podBurstTime, 10)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			klog.ErrorS(nil, "CPU Burst is not supported", "cgroupFile", podQuotaBurstFile)
			return nil
		}
		return err
	}
	klog.InfoS("Successfully set pod cpu quota burst time", "path", podQuotaBurstFile, "quotaBurst", podBurstTime, "pod", klog.KObj(pod))
	return nil
}

func walkFunc(cgroupPath string, quotaBurstTime int64, podBurstTime *int64, cgroupVersion string) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// We will set pod cgroup later.
		if path == cgroupPath {
			return nil
		}
		if d == nil || !d.IsDir() {
			return nil
		}

		var quotaTotalFile, quotaBurstFile string
		if cgroupVersion == cgroup.CgroupV2 {
			quotaTotalFile = filepath.Join(path, cgroup.CPUQuotaTotalFileV2)
			quotaBurstFile = filepath.Join(path, cgroup.CPUQuotaBurstFileV2)
		} else {
			quotaTotalFile = filepath.Join(path, cgroup.CPUQuotaTotalFile)
			quotaBurstFile = filepath.Join(path, cgroup.CPUQuotaBurstFile)
		}

		quotaTotal, err := file.ReadIntFromFile(quotaTotalFile)
		if err != nil {
			return fmt.Errorf("failed to get container cpu total quota time, err: %v, path: %s", err, quotaTotalFile)
		}
		if quotaTotal == fixedQuotaValue {
			return nil
		}

		actualBurst := quotaBurstTime
		if quotaBurstTime > quotaTotal {
			klog.ErrorS(nil, "The quota burst time is greater than quota total, use quota total as burst time", "quotaBurst", quotaBurstTime, "quoTotal", quotaTotal)
			actualBurst = quotaTotal
		}
		if quotaBurstTime == 0 {
			actualBurst = quotaTotal
		}
		*podBurstTime += actualBurst
		err = utils.UpdateFile(quotaBurstFile, []byte(strconv.FormatInt(actualBurst, 10)))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				klog.ErrorS(nil, "CPU Burst is not supported", "cgroupFile", quotaBurstFile)
				return nil
			}
			return err
		}

		klog.InfoS("Successfully set container cpu burst time", "path", quotaBurstFile, "quotaTotal", quotaTotal, "quotaBurst", actualBurst)
		return nil
	}
}
