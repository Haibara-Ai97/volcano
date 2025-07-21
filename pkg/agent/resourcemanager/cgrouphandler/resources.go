package cgrouphandler

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/utils"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
)

func (c *CgroupHandler) SetResourceLimit(podEvent framework.PodEvent) error {
	resources := []utilpod.Resources{}
	switch c.cgroupVersion {
	case "v1":
		resources = utilpod.CalculateExtendResources(podEvent.Pod)
	case "v2":
		resources = utilpod.CalculateExtendResourcesV2(podEvent.Pod)
	default:
		return fmt.Errorf("unsupport cgroup version: %s", c.cgroupVersion)
	}
	var errs []error
	for _, cr := range resources {
		cgroupPath, err := c.cgroupMgr.GetPodCgroupPath(podEvent.QoSClass, cr.CgroupSubSystem, podEvent.UID)
		if err != nil {
			klog.ErrorS(err, "Failed to get pod cgroup", "pod", klog.KObj(podEvent.Pod), "subSystem", cr.CgroupSubSystem)
			errs = append(errs, err)
		}

		filePath := path.Join(cgroupPath, cr.ContainerID, cr.SubPath)
		// 只在 cgroup v2 下处理 -1 特殊值，写入 "max"
		if c.cgroupVersion == "v2" && cr.Value == -1 {
			if cr.SubPath == cgroup.CPUQuotaTotalFileV2 {
				// cpu.max: "max 100000"
				err = utils.UpdateFile(filePath, []byte(fmt.Sprint("max 100000")))
			} else {
				// memory.max: "max"
				err = utils.UpdateFile(filePath, []byte("max"))
			}
		} else {
			err = utils.UpdateFile(filePath, []byte(strconv.FormatInt(cr.Value, 10)))
		}
		if os.IsNotExist(err) {
			klog.InfoS("Cgroup file not existed", "filePath", filePath)
			continue
		}

		if err != nil {
			errs = append(errs, err)
			klog.ErrorS(err, "Failed to set cgroup", "path", filePath, "pod", klog.KObj(podEvent.Pod))
			continue
		}
		klog.InfoS("Successfully set cpu and memory cgroup", "path", filePath, "pod", klog.KObj(podEvent.Pod))
	}
	return utilerrors.NewAggregate(errs)
}
