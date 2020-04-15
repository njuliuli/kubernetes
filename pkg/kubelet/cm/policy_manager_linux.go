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

const (
	// policy names for getPodPolicy(pod), e.g. v1.Pod.Spec.Policy, now supported
	policyUnknown  = "unknown"
	policyDefault  = ""
	policyCPUCFS   = "policy-cpu-cfs"
	policyIsolated = "policy-isolated"
)

// getPodPolicy return per-task policy,
// which is used by PolicyManager for pod level cgroup values enforcement
func getPodPolicy(pod *v1.Pod) string {
	return pod.Spec.Policy
}

type policyManagerImpl struct {
	// Protect the entire PolicyManager, including any Cgroup.
	// TODO(li) I should write some tests to confirm thread-safe for exported methods.
	mutex sync.Mutex

	// Track all pods managed by PolicyManager, map[string(pod.UID)] -> pod.
	// Now pod update is not allowed.
	// TODO(li) We may use type sets.String instead.
	uidToPod map[string]*v1.Pod

	// Each Cgroup struct is used to manage pod level cgroup values for a purpose

	// Pod-local, for CFS related cgroup values
	cgroupCPUCFS Cgroup
	// Host-global, for cpuset related cgroup values
	cgroupCPUSet Cgroup

	// Interface for cgroup management
	cgroupManager CgroupManager
	// newPodContainerManager is a factory method returns PodContainerManager,
	// which is the interface to stores and manages pod level containers.
	// We use factory method since ContainerManager do it this way.
	newPodContainerManager typeNewPodContainerManager
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

	cgroupCPUCFS, err := newCgroupCPUCFS()
	if err != nil {
		return nil, fmt.Errorf("fail to create cgroupCPUCFS, %q", err)
	}
	cgroupCPUSet, err := newCgroupCPUSet(cpuTopology,
		cpumanager.TakeByTopology, cpusSpecific, nodeAllocatableReservation)
	if err != nil {
		return nil, fmt.Errorf("fail to create cgroupCPUSet, %q", err)
	}

	pm := &policyManagerImpl{
		uidToPod:               make(map[string]*v1.Pod),
		cgroupCPUCFS:           cgroupCPUCFS,
		cgroupCPUSet:           cgroupCPUSet,
		cgroupManager:          cgroupManager,
		newPodContainerManager: newPodContainerManager,
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

func (p *policyManagerImpl) AddPod(pod *v1.Pod) error {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}
	klog.Infof("[policymanager] add pod (%q), with policy (%q) and UID (%q)",
		pod.Name, getPodPolicy(pod), pod.UID)

	// A pod can only be added once, update is not supported for now
	podUID := string(pod.UID)
	if _, found := p.uidToPod[podUID]; found {
		return fmt.Errorf("pod already added to PolicyManager")
	}
	p.uidToPod[podUID] = pod

	// Continue all following steps even if dependencies failed.
	isFailed := false

	// Write to some Cgroup according to per-task policy,
	// then read the current cgroup values from them.
	switch getPodPolicy(pod) {
	// For those policies, add pod to all Cgroup.
	case policyDefault, policyIsolated, policyCPUCFS:
		if err := p.addPodAllCgroup(pod); err != nil {
			klog.Infof("[policymanager] AddPod fails with error\n %v", err)
			isFailed = true
		}
	default:
		return fmt.Errorf("policy (%q) is not supported", getPodPolicy(pod))
	}

	if err := p.updateToHost(podUID); err != nil {
		klog.Infof("[policymanager] AddPod fails with error\n %v", err)
		isFailed = true
	}

	if isFailed {
		return fmt.Errorf("per-task policy AddPod failed for reasons above")
	}

	return nil
}

// addPodAllCgroup add pod to all Cgroup for cgroup values management
func (p *policyManagerImpl) addPodAllCgroup(pod *v1.Pod) error {
	isFailed := false
	// For pod-local Cgroup
	if err := p.cgroupCPUCFS.AddPod(pod); err != nil {
		klog.Infof("[policymanager] add to cgroupCPUCFS fails with error\n %v", err)
		isFailed = true
	}

	// For host-global Cgroup
	if err := p.cgroupCPUSet.AddPod(pod); err != nil {
		klog.Infof("[policymanager] add to cgroupCPUSet fails with error\n %v", err)
		isFailed = true
	}

	if isFailed {
		return fmt.Errorf("addPodAllCgroup failed for reasons above")
	}

	return nil
}

// TODO(li) For now, there are cases when adding pod to Cgroup fails,
// such as, for CPUSet, pod is too large or dedicated number is 0,
// for CPUCFS, mode is unknown.
// But in those cases, the failed pod is tracked,
// and default value will be returned in the next writeHost step.
func (p *policyManagerImpl) updateToHost(podUID string) error {
	// resourceConfig := &resourceConfig{}

	// if rc, err := p.cgroupCPUCFS.ReadPod(podUID); err != nil {
	// 	klog.Infof("add to cgroupCPUCFS fails with error\n %v", err)
	// 	isFailed = true
	// }

	return nil
}

func (p *policyManagerImpl) RemovePod(pod *v1.Pod) error {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}
	klog.Infof("[policymanager] remove pod (%q) with UID (%q)",
		pod.Name, pod.UID)

	// A pod can only be added once, update is not supported for now
	podUID := string(pod.UID)
	if _, found := p.uidToPod[podUID]; !found {
		return fmt.Errorf("pod not added to PolicyManager yet")
	}
	delete(p.uidToPod, podUID)

	// Continue all following steps even if dependencies failed.
	isFailed := false

	if err := p.removeAllCgroup(podUID); err != nil {
		klog.Infof("[policymanager] RemovePod fails with error\n %v", err)
		isFailed = true
	}

	if err := p.updateToHost(podUID); err != nil {
		klog.Infof("[policymanager] RemovePod fails with error\n %v", err)
		isFailed = true
	}

	if isFailed {
		return fmt.Errorf("RemovePod failed for reasons above")
	}

	return nil
}

func (p *policyManagerImpl) removeAllCgroup(podUID string) error {
	isFailed := false
	// For pod-local Cgroup
	if err := p.cgroupCPUCFS.RemovePod(podUID); err != nil {
		klog.Infof("[policymanager] remove from cgroupCPUCFS fails with error\n %v", err)
		isFailed = true
	}

	// For host-global Cgroup
	if err := p.cgroupCPUSet.RemovePod(podUID); err != nil {
		klog.Infof("[policymanager] remove from cgroupCPUSet fails with error\n %v", err)
		isFailed = true
	}

	if isFailed {
		return fmt.Errorf("removeAllCgroup failed for reasons above")
	}
	return nil
}
