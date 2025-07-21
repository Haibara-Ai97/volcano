/*
Copyright 2024 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cpuburst

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/resourcemanager"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.PodEventName), NewCPUBurst)
}

type CPUBurstHandle struct {
	*base.BaseHandle
	cgroupMgr       cgroup.CgroupManager
	podInformer     v1.PodInformer
	resourceHandler resourcemanager.ResourceHandler
}

func NewCPUBurst(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager, resourceMgr *resourcemanager.ResourceManager) framework.Handle {
	handler := resourceMgr.Handler
	return &CPUBurstHandle{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.CPUBurstFeature),
			Config: config,
		},
		cgroupMgr:       cgroupMgr,
		podInformer:     config.InformerFactory.K8SInformerFactory.Core().V1().Pods(),
		resourceHandler: handler,
	}
}

func (c *CPUBurstHandle) Handle(event interface{}) error {
	podEvent, ok := event.(framework.PodEvent)
	if !ok {
		return fmt.Errorf("illegal pod event")
	}

	// Get latest pod information from informer
	pod := podEvent.Pod
	latestPod, err := c.podInformer.Lister().Pods(pod.Namespace).Get(pod.Name)
	if err != nil {
		klog.ErrorS(err, "Failed to get pod from lister")
	} else {
		pod = latestPod
	}
	str, exists := pod.Annotations[EnabledKey]
	if !exists {
		return nil
	}
	enable, err := strconv.ParseBool(str)
	if err != nil || !enable {
		return nil
	}

	quotaBurstTime := getCPUBurstTime(pod)

	// Set CPU burst
	return c.resourceHandler.SetCPUBurst(podEvent.QoSClass, podEvent.UID, quotaBurstTime, pod)
}

func getCPUBurstTime(pod *corev1.Pod) int64 {
	var quotaBurstTime int64
	str, exists := pod.Annotations[QuotaTimeKey]
	if !exists {
		return quotaBurstTime
	}
	value, err := strconv.ParseInt(str, 10, 64)
	if err != nil || value <= 0 {
		klog.ErrorS(err, "Invalid quota burst time, use default containers' quota time", "value", str)
		return quotaBurstTime
	}
	quotaBurstTime = int64(value)
	return quotaBurstTime
}
