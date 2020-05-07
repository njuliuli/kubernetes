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

	"github.com/imdario/mergo"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
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
	// Protect the entire PolicyManager,
	// including all external calls for all read or write on Cgroup,
	// making PolicyManager atomic.
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

	// activePodsFunc is a method for listing active pods on the node
	activePodsFunc ActivePodsFunc
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

func (pm *policyManagerImpl) Start(activePodsFunc ActivePodsFunc) (rerr error) {
	klog.Infof("[policymanager] Start policyManagerImpl, %+v", pm)

	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if err := pm.cgroupCPUCFS.Start(); err != nil {
		return fmt.Errorf("fail to start cgroupCPUCFS; %q", err)
	}
	if err := pm.cgroupCPUSet.Start(); err != nil {
		return fmt.Errorf("fail to start cgroupCPUSet; %q", err)
	}

	pm.activePodsFunc = activePodsFunc

	return nil
}

func (pm *policyManagerImpl) AddPod(pod *v1.Pod) error {
	if pod == nil {
		klog.Infof("[policymanager] AddPod on pod (nil), skip")
		return nil
	}
	klog.Infof("[policymanager] AddPod on pod (%q), with policy (%q) and UID (%q)",
		pod.Name, getPodPolicy(pod), pod.UID)

	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Continue all following steps even if dependencies failed.
	isFailed := false

	// Update PolicyManager to the current state of runtime.
	uidToRemove, podToAdd := pm.getPodsForAdd(pod)
	if err := pm.update(uidToRemove, podToAdd); err != nil {
		klog.Infof("[policymanager] update fails with error\n %v", err)
		isFailed = true
	}

	if err := pm.updateToHost(); err != nil {
		klog.Infof("[policymanager] updateToHost fails with error\n %v", err)
		isFailed = true
	}

	if isFailed {
		return fmt.Errorf("AddPod fails with errors above")
	}
	return nil
}

// Get pods to update in PolicyManager for AddPod().
// Since sometimes errors prevent kubelet to completing lifecycle of a pod,
// then global state in PolicyManager will be different from actual state.
// For example, kubelet may recover from crash, causing losing of previous states.
// Then when a new pod is added,
// there are pods other than this new pod should be updated in PolicyManager.
func (pm *policyManagerImpl) getPodsForAdd(pod *v1.Pod) (uidToRemove sets.String, podToAdd map[string]*v1.Pod) {
	activePods := pm.activePodsFunc()

	// Remove all pods in PolicyManager that are not active
	uidToRemove = sets.NewString()
	for podUID := range pm.uidToPod {
		if !pm.isUIDInPodArray(podUID, activePods) {
			uidToRemove.Insert(podUID)
		}
	}

	podToAdd = make(map[string]*v1.Pod)
	// Add all active pods to PolicyManager that are not already inside
	// Note this pod not added if not in activePods, when cgroup path may not exit
	for _, p := range activePods {
		if _, found := pm.uidToPod[string(p.UID)]; !found {
			podToAdd[string(p.UID)] = p
		}
	}

	return uidToRemove, podToAdd
}

// updateToHost read all Cgroup and then write to the host.
// TODO(li) There are cases when adding pod to Cgroup returns errors,
// such as, for CPUSet, pod is too large or dedicated number is 0,
// For now, the failed pod is still added,
// and the default value (cpusShared for CPUSet) will be written to host.
func (pm *policyManagerImpl) updateToHost() error {
	pcm := pm.newPodContainerManager()

	// Continue all following steps even if dependencies failed.
	isFailed := false

	// Update all pods tracked by PolicyManager
	for _, pod := range pm.uidToPod {
		resourceConfig := &ResourceConfig{}

		// Read all cgroup values from all Cgroups
		rc, isTracked := pm.cgroupCPUCFS.ReadPod(pod)
		if !isTracked {
			klog.Infof("[policymanager] cgroupCPUCFS, pod not added yet")
			isFailed = true
		}
		// When resourceConfig and rc have non-zero value in the same field,
		// the only in resourceConfig is kept.
		// But this should never happens,
		// as different Cgroup manage different fields of ResourceConfig.
		if err := mergo.Merge(resourceConfig, rc); err != nil {
			klog.Infof("[policymanager] cgroupCPUCFS, fails in merging two ResourceConfig\n %+v\n %+v",
				resourceConfig, rc)
			isFailed = true
		}

		rc, isTracked = pm.cgroupCPUSet.ReadPod(pod)
		if !isTracked {
			klog.Infof("[policymanager] cgroupCPUSet, pod not added yet")
			isFailed = true
		}
		if err := mergo.Merge(resourceConfig, rc); err != nil {
			klog.Infof("[policymanager] cgroupCPUSet, fails in merging two ResourceConfig\n %+v\n %+v",
				resourceConfig, rc)
			isFailed = true
		}

		// Write to host
		cgroupName, _ := pcm.GetPodContainerName(pod)
		cgroupConfig := &CgroupConfig{
			Name:               cgroupName,
			ResourceParameters: resourceConfig,
		}
		if err := pm.cgroupManager.Update(cgroupConfig); err != nil {
			klog.Infof("[policymanager] CgroupManager.Update() fails with error (%v)",
				err)
			isFailed = true
		}
	}

	if isFailed {
		return fmt.Errorf("updateToHost failed for reasons above")
	}
	return nil
}

func (pm *policyManagerImpl) RemovePod(pod *v1.Pod) error {
	if pod == nil {
		klog.Infof("[policymanager] RemovePod on pod (nil), skip")
		return nil
	}
	klog.Infof("[policymanager] RemovePod on pod (%q), with policy (%q) and UID (%q)",
		pod.Name, getPodPolicy(pod), pod.UID)

	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Continue all following steps even if dependencies failed.
	isFailed := false

	// Update PolicyManager
	uidToRemove, podToAdd := pm.getPodsForRemove(pod)
	if err := pm.update(uidToRemove, podToAdd); err != nil {
		klog.Infof("[policymanager] update fails with error\n %v", err)
		isFailed = true
	}

	if err := pm.updateToHost(); err != nil {
		klog.Infof("[policymanager] updateToHost fails with error\n %v", err)
		isFailed = true
	}

	if isFailed {
		return fmt.Errorf("RemovePod fails with errors above")
	}
	return nil
}

// Get pods to update in PolicyManager for AddPod()
func (pm *policyManagerImpl) getPodsForRemove(pod *v1.Pod) (uidToRemove sets.String, podToAdd map[string]*v1.Pod) {
	activePods := pm.activePodsFunc()

	uidToRemove = sets.NewString()
	// Remove this pod if exists, which may or may not in activePods
	if _, found := pm.uidToPod[string(pod.UID)]; found {
		uidToRemove.Insert(string(pod.UID))
	}
	// Remove all pods in PolicyManager that are not active
	for podUID := range pm.uidToPod {
		if !pm.isUIDInPodArray(podUID, activePods) {
			uidToRemove.Insert(podUID)
		}
	}

	// Add all active pods to PolicyManager that are not already inside
	podToAdd = make(map[string]*v1.Pod)
	for _, p := range activePods {
		// Exclude the current pod that is to be removed
		if _, found := pm.uidToPod[string(p.UID)]; !found && p.UID != pod.UID {
			podToAdd[string(p.UID)] = p
		}
	}

	return uidToRemove, podToAdd
}

// check if the given podUID match any active pods
func (pm *policyManagerImpl) isUIDInPodArray(podUID string, podArray []*v1.Pod) bool {
	for _, pod := range podArray {
		if string(pod.UID) == podUID {
			return true
		}
	}
	return false
}

// Update is used to udpate the Cgroups in PolicyManager
func (pm *policyManagerImpl) update(uidToRemove sets.String, podToAdd map[string]*v1.Pod) error {
	// Continue all following steps even if dependencies failed.
	isFailed := false

	for podUID := range uidToRemove {
		// Remove from PolicyManager even if some Cgroup fails
		delete(pm.uidToPod, podUID)
		klog.Infof("[policymanager] remove pod uid (%q)", podUID)

		if err := pm.cgroupCPUCFS.RemovePod(podUID); err != nil {
			klog.Infof("[policymanager] remove from cgroupCPUCFS fails with error\n %v", err)
			isFailed = true
		}
		if err := pm.cgroupCPUSet.RemovePod(podUID); err != nil {
			klog.Infof("[policymanager] remove from cgroupCPUSet fails with error\n %v", err)
			isFailed = true
		}
	}

	for podUID, pod := range podToAdd {
		// Add to PolicyManager even if some Cgroup fails
		pm.uidToPod[podUID] = pod
		klog.Infof("[policymanager] add pod (%q), uid (%q)",
			pod.Name, podUID)

		// The per-task policy is implemented at each Cgroup.
		// For example, when a policy is not supported by a Cgroup, it returns error.
		if err := pm.cgroupCPUCFS.AddPod(pod); err != nil {
			klog.Infof("[policymanager] add to cgroupCPUCFS fails with error\n %v", err)
			isFailed = true
		}
		if err := pm.cgroupCPUSet.AddPod(pod); err != nil {
			klog.Infof("[policymanager] add to cgroupCPUSet fails with error\n %v", err)
			isFailed = true
		}
	}

	if isFailed {
		return fmt.Errorf("update fails with errors above")
	}
	return nil
}
