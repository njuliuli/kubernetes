/*
Copyright 2020 The Kubernetes Authors.

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

package cm

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
)

// cgroupCPUSet is used to manage all cpuset related cgroup values,
// such as cpuset.cpus.
type cgroupCPUSet struct {
	// Track all pods added to cgroupCPUSet, set of pod.UID
	podSet sets.String
}

var _ Cgroup = &cgroupCPUSet{}

// NewCgroupCPUSet creates cgroupCPUSet
func NewCgroupCPUSet() (Cgroup, error) {
	klog.Infof("[policymanager] Create cgroupCPUSet")

	ccs := &cgroupCPUSet{
		podSet: sets.NewString(),
	}

	return ccs, nil
}

func (ccs *cgroupCPUSet) Start() (rerr error) {
	klog.Infof("[policymanager] Start cgroupCPUSet, %+v", ccs)

	return nil
}

func (ccs *cgroupCPUSet) AddPod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}

	klog.Infof("[policymanager] Add pod (%q) to cgroupCPUSet", pod.Name)

	// A pod can only be added once, update is not supported for now
	podUID := string(pod.UID)
	if ccs.podSet.Has(podUID) {
		return fmt.Errorf("pod (%q) already added to cgroupCPUSet", pod.Name)
	}
	ccs.podSet.Insert(podUID)

	klog.Infof("[policymanager] cgroupCPUSet (%+v) after AddPod", ccs)

	return nil
}

func (ccs *cgroupCPUSet) RemovePod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}

	klog.Infof("[policymanager] Remove pod (%q) from cgroupCPUSet", pod.Name)

	podUID := string(pod.UID)
	if !ccs.podSet.Has(podUID) {
		return fmt.Errorf("pod (%q) not added to cgroupCPUSet yet", pod.Name)
	}
	ccs.podSet.Delete(podUID)

	klog.Infof("[policymanager] cgroupCPUSet (%+v) after RemovePod", ccs)

	return nil
}
