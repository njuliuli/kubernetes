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
	policyDefault  = ""
	policyCPUCFS   = "policy-cpu-cfs"
	policyIsolated = "policy-isolated"
	policyUnknown  = "unknown"

	// action name for calling updatePodInCgroup(...)
	actionAddPod    = "add-pod"
	actionRemovePod = "remove-pod"
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

	// Each Cgroup struct is used to manage pod level cgroup values for a purpose

	// Pod-local, for CFS related cgroup values
	cgroupCPUCFS Cgroup
	// Host-global, for cpuset related cgroup values
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
	// TODO(li) For now, there are cases when adding pod to Cgroup fails,
	// such as, for CPUSet, pod is too large or dedicated number is 0,
	// for CPUCFS, mode is unknown.
	// But in those cases, the failed pod is tracked,
	// and default value will be returned in the next writeHost step.
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
func updatePodInCgroup(pod *v1.Pod, cgroup Cgroup, action string, mode string) (rerr error) {
	switch action {
	case actionAddPod:
		rerr = cgroup.AddPod(pod, mode)
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
	klog.Infof("[policymanager] updatePodByPolicy, pod (%q) with policy (%q) and UID (%q), action (%q)",
		pod.Name, getPodPolicy(pod), pod.UID, action)

	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Write to some Cgroup according to per-task policy,
	// iterate all Cgroup even if some failed
	isFailed := false
	// For some pods with specific policy, update specific pod-local Cgroup
	switch getPodPolicy(pod) {
	case policyDefault, policyIsolated:
		klog.Infof("[policymanager] No special action for pod (%q)", pod.Name)
	case policyCPUCFS:
		klog.Infof("[policymanager] Update cgroupCPUCFS for pod (%q), mode (%q)",
			pod.Name, modeCPUCFSDefault)
		if err := updatePodInCgroup(pod, p.cgroupCPUCFS, action, modeCPUCFSDefault); err != nil {
			klog.Infof("cgroupCPUCFS fails with error\n %v", err)
			isFailed = true
		}
	default:
		return fmt.Errorf("policy (%q) of pod (%q) is unkonwn",
			getPodPolicy(pod), pod.Name)
	}

	// For all pods, update host-global Cgroup
	modeCPUSet := modeCPUSetDefault
	if getPodPolicy(pod) == policyIsolated {
		modeCPUSet = modeCPUSetDedicated
	}
	klog.Infof("[policymanager] Update cgroupCPUSet for pod (%q), mode (%q)",
		pod.Name, modeCPUSet)
	if err := updatePodInCgroup(pod, p.cgroupCPUSet, action, modeCPUSet); err != nil {
		klog.Infof("cgroupCPUSet fails with error\n %v", err)
		isFailed = true
	}

	if isFailed {
		return fmt.Errorf("Per-task policy update to Cgroup failed")
	}

	return nil
}
