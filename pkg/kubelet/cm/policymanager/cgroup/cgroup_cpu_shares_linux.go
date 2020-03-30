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

package cgroup

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
)

type cgroupCPUShares struct {
	// Track all pods added to cgroupCPUShares, set of pod.UID
	podSet sets.String
}

var _ Cgroup = &cgroupCPUShares{}

// NewCgroupCpuShares creates state for cpu.shares
func NewCgroupCPUShares() (Cgroup, error) {
	klog.Infof("[policymanager] Create cgroupCPUShares")

	ccs := &cgroupCPUShares{
		podSet: sets.NewString(),
	}

	return ccs, nil
}

func (ccs *cgroupCPUShares) Start() (rerr error) {
	klog.Infof("[policymanager] Start cgroupCPUShares, %+v", ccs)

	return nil
}

func (ccs *cgroupCPUShares) AddPod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}

	klog.Infof("[policymanager] Add pod (Name = %q) to cgroupCPUShares", pod.Name)

	// A pod can only be added once, update is not supported for now
	podUID := string(pod.UID)
	if ccs.podSet.Has(podUID) {
		return fmt.Errorf("pod (Name = %q) already added to cgroupCPUShares", pod.Name)
	}
	ccs.podSet.Insert(podUID)

	return nil
}

func (ccs *cgroupCPUShares) RemovePod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}

	klog.Infof("[policymanager] Remove pod from cgroupCPUShares, %q", pod.Name)

	if !ccs.podSet.Has(string(pod.UID)) {
		return fmt.Errorf("pod (Name = %q) not added to cgroupCPUShares yet", pod.Name)
	}
	ccs.podSet.Delete(string(pod.UID))

	return nil
}
