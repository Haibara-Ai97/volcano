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

package resources

import (
	"fmt"
	"volcano.sh/volcano/pkg/agent/resourcemanager"

	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	handlers.RegisterEventHandleFunc(string(framework.PodEventName), NewResources)
}

type ResourcesHandle struct {
	*base.BaseHandle
	resourceHandler resourcemanager.ResourceHandler
	cgroupMgr       cgroup.CgroupManager
}

func NewResources(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, cgroupMgr cgroup.CgroupManager, resourceMgr *resourcemanager.ResourceManager) framework.Handle {
	return &ResourcesHandle{
		BaseHandle: &base.BaseHandle{
			Name:   string(features.ResourcesFeature),
			Config: config,
			Active: true,
		},
		cgroupMgr:       cgroupMgr,
		resourceHandler: resourceMgr.Handler,
	}
}

func (r *ResourcesHandle) Handle(event interface{}) error {
	podEvent, ok := event.(framework.PodEvent)
	if !ok {
		return fmt.Errorf("illegal pod event")
	}

	if !allowedUseExtRes(podEvent.QoSLevel) {
		return nil
	}

	return r.resourceHandler.SetResourceLimit(podEvent)
}

// allowedUseExtRes defines what qos levels can use extension resources,
// currently only qos level QosLevelLS and QosLevelBE can use.
func allowedUseExtRes(qosLevel int64) bool {
	return qosLevel <= 1
}
