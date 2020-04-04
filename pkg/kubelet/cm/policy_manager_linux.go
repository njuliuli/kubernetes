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
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"
)

// policy names for v1.Pod.Spec.Policy
const (
	policyDefault = ""
	policyNone    = "none"
)

type policyManagerImpl struct {
	// Protect the entire PolicyManager, including any Cgroup.
	// TODO(li) I should write some tests to confirm thread-safe for exported methods.
	mutex sync.Mutex

	// Each Cgroup struct is used to manage cgroup values for a purpose
	cgroupCPUCFS Cgroup
}

var _ PolicyManager = &policyManagerImpl{}

// NewPolicyManager creates PolicyManager for pod level cgroup values
func NewPolicyManager(cgroupManager CgroupManager,
	newPodContainerManager typeNewPodContainerManager) (PolicyManager, error) {
	klog.Infof("[policymanager] Create policyManagerImpl")

	cgroupCPUCFS, err := NewCgroupCPUCFS(cgroupManager, newPodContainerManager)
	if err != nil {
		return nil, fmt.Errorf("fail to create cgroupCPUCFS, %q", err)
	}

	pm := &policyManagerImpl{
		cgroupCPUCFS: cgroupCPUCFS,
	}

	return pm, nil
}

func (p *policyManagerImpl) Start() (rerr error) {
	klog.Infof("[policymanager] Start policyManagerImpl, %+v", p)

	if err := p.cgroupCPUCFS.Start(); err != nil {
		return fmt.Errorf("fail to start cgroupCPUCFS; %q", err)
	}

	return nil
}

func (p *policyManagerImpl) AddPod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}
	klog.Infof("[policymanager] Add pod (%q) with policy name (%q) to policyManagerImpl",
		pod.Name, pod.Spec.Policy)

	p.mutex.Lock()
	defer p.mutex.Unlock()

	// TODO(li) Write to some Cgroup according to per-task policy
	switch pod.Spec.Policy {
	case policyDefault:
		if err := p.cgroupCPUCFS.AddPod(pod); err != nil {
			return fmt.Errorf("fail to add pod to policyManagerImpl; %q", err)
		}
	case policyNone:
		klog.Infof("[policymanager] Skip pod (%q) with policy name (%q) to policyManagerImpl",
			pod.Name, pod.Spec.Policy)
	default:
		return fmt.Errorf("pod (Name = %q) policy name (%q) is unkonwn",
			pod.Name, pod.Spec.Policy)
	}
	return nil
}

func (p *policyManagerImpl) RemovePod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}
	klog.Infof("[policymanager] Remove pod from policyManagerImpl, %q", pod.Name)

	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Just remove pod from all Cgroup, not limited to per-task policy ones,
	// as those will be skipped as this pod is not added to them in AddPod()

	if err := p.cgroupCPUCFS.RemovePod(pod); err != nil {
		return fmt.Errorf("fail to remove pod from policyManagerImpl; %q", err)
	}

	return nil
}
