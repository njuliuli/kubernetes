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
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager"
	cputopology "k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

// policy names for v1.Pod.Spec.Policy
const (
	policyDefault = ""
	policyCPUCFS  = "cfs"
	policyCPUSet  = "cpuset"
	policyUnknown = "unknown"

	actionAddPod    = "add-pod"
	actionRemovePod = "remove-pod"
)

type policyManagerImpl struct {
	// Protect the entire PolicyManager, including any Cgroup.
	// TODO(li) I should write some tests to confirm thread-safe for exported methods.
	mutex sync.Mutex

	// Each Cgroup struct is used to manage pod level cgroup values for a purpose

	// To manage CFS related cgroup values
	cgroupCPUCFS Cgroup
	// To manage cpuset related cgroup values
	cgroupCPUSet Cgroup
}

var _ PolicyManager = &policyManagerImpl{}

// NewPolicyManager creates PolicyManager for pod level cgroup values
func NewPolicyManager(newCgroupCPUCFS typeNewCgroupCPUCFS,
	newCgroupCPUSet typeNewCgroupCPUSet,
	cgroupManager CgroupManager,
	newPodContainerManager typeNewPodContainerManager,
	cpuTopology *cputopology.CPUTopology,
	cpusSpecific cpuset.CPUSet,
	nodeAllocatableReservation v1.ResourceList) (PolicyManager, error) {
	klog.Infof("[policymanager] Create policyManagerImpl")

	cgroupCPUCFS, err := newCgroupCPUCFS(cgroupManager, newPodContainerManager)
	if err != nil {
		return nil, fmt.Errorf("fail to create cgroupCPUCFS, %q", err)
	}
	cgroupCPUSet, err := newCgroupCPUSet(cpuTopology,
		cpumanager.TakeByTopology, cpusSpecific, nodeAllocatableReservation)
	if err != nil {
		return nil, fmt.Errorf("fail to create cgroupCPUSet, %q", err)
	}

	pm := &policyManagerImpl{
		cgroupCPUCFS: cgroupCPUCFS,
		cgroupCPUSet: cgroupCPUSet,
	}

	return pm, nil
}

func (p *policyManagerImpl) Start() (rerr error) {
	klog.Infof("[policymanager] Start policyManagerImpl, %+v", p)

	if err := p.cgroupCPUCFS.Start(); err != nil {
		return fmt.Errorf("fail to start cgroupCPUCFS; %q", err)
	}
	if err := p.cgroupCPUSet.Start(); err != nil {
		return fmt.Errorf("fail to start cgroupCPUSet; %q", err)
	}

	return nil
}

func (p *policyManagerImpl) AddPod(pod *v1.Pod) (rerr error) {
	if err := p.updatePodByPolicy(pod, actionAddPod); err != nil {
		return err
	}

	return nil
}

func (p *policyManagerImpl) RemovePod(pod *v1.Pod) (rerr error) {
	if err := p.updatePodByPolicy(pod, actionRemovePod); err != nil {
		return err
	}

	return nil
}

// updatePod add/remove pod using Cgroup.AddPod/RemovePod()
func updatePodInCgroup(pod *v1.Pod, cgroup Cgroup, action string) (rerr error) {
	switch action {
	case actionAddPod:
		rerr = cgroup.AddPod(pod)
	case actionRemovePod:
		rerr = cgroup.RemovePod(pod)
	default:
		return fmt.Errorf("action (%q) should be in set {%q, %q}",
			action, actionAddPod, actionRemovePod)
	}

	return rerr
}

// updatePodByPolicy add/remove pod in Cgroup values based on per-task policy
func (p *policyManagerImpl) updatePodByPolicy(pod *v1.Pod, action string) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}
	klog.Infof("[policymanager] update pod (%q) with policy (%q) and UID (%q) in policyManagerImpl",
		pod.Name, pod.Spec.Policy, pod.UID)

	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Write to some Cgroup according to per-task policy
	switch pod.Spec.Policy {
	case policyDefault:
		klog.Infof("[policymanager] Skip pod (%q) with policy (%q)",
			pod.Name, pod.Spec.Policy)
	case policyCPUCFS:
		if err := updatePodInCgroup(pod, p.cgroupCPUCFS, action); err != nil {
			return fmt.Errorf("action (%q) to add pod (%q) error: %v",
				action, pod.Name, err)
		}
	case policyCPUSet:
		if err := updatePodInCgroup(pod, p.cgroupCPUSet, action); err != nil {
			return fmt.Errorf("action (%q) to add pod (%q) error: %v",
				action, pod.Name, err)
		}
	default:
		return fmt.Errorf("policy (%q) of pod (%q) is unkonwn",
			pod.Spec.Policy, pod.Name)
	}

	// TODO(li) Should we just remove pod from all Cgroup,
	// not limited to per-task policy ones?
	// as those will be skipped as this pod is not added to them in AddPod().
	// Also, should we write a policy function to be reused by AddPod and RemovePod?

	return nil
}
