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
	"k8s.io/klog"
)

type policyManagerImpl struct {
	// stateArray stores all states for each cgroup value
	cgroupArray []Cgroup
}

var _ PolicyManager = &policyManagerImpl{}

// NewPolicyManager creates PolicyManager for pod level cgroup values
func NewPolicyManager() (PolicyManager, error) {
	klog.Infof("[policymanager] Create policyManagerImpl")

	var ca []Cgroup
	if ccc, err := NewCgroupCPUCFS(); err != nil {
		return nil, fmt.Errorf("fail to create cgroupCPUCFS, %q", err)
	} else {
		ca = append(ca, ccc)
	}

	pm := &policyManagerImpl{
		cgroupArray: ca,
	}

	return pm, nil
}

func (p *policyManagerImpl) Start() (rerr error) {
	klog.Infof("[policymanager] Start policyManagerImpl, %+v", p)

	for _, c := range p.cgroupArray {
		if err := c.Start(); err != nil {
			return fmt.Errorf("fail to start cgroupCPUCFS; %q", err)
		}
	}

	return nil
}

func (p *policyManagerImpl) AddPod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}
	klog.Infof("[policymanager] Add pod to policyManagerImpl, %q", pod.Name)

	for _, c := range p.cgroupArray {
		if err := c.AddPod(pod); err != nil {
			return fmt.Errorf("fail to add pod to policyManagerImpl; %q", err)
		}
	}

	return nil
}

func (p *policyManagerImpl) RemovePod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}
	klog.Infof("[policymanager] Remove pod from policyManagerImpl, %q", pod.Name)

	for _, c := range p.cgroupArray {
		if err := c.RemovePod(pod); err != nil {
			return fmt.Errorf("fail to remove pod from policyManagerImpl; %q", err)
		}
	}

	return nil
}
