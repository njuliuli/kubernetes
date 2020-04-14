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
	"math"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/api/v1/resource"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	cputopology "k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

// cgroupCPUSet is used to manage all cpuset related cgroup values,
// such as cpuset.cpus.
type cgroupCPUSet struct {
	// Track all pods added to cgroupCPUSet, set of pod.UID
	// TODO(li) Decide whether to remove podSet, when implementing writing to host.
	// It seems any pod that is not found in podToCPUS is considered the same,
	// whether they are in or not in podSet.
	podSet sets.String

	// Contains details of node CPU topology.
	cpuTopology *topology.CPUTopology

	// cpusReserved is not for pods, but for kubelet and system.
	cpusReserved cpuset.CPUSet
	// Below are used to keep track of cpuset.cpus available to pods.
	// podToCPUS is used to track dedicated cpuset assignment for some pods,
	// while cpusShared is used to track the shared cpuset for all other pods.
	// podToCPUS is of map[string(pod.UID)] -> CPUSet
	cpusShared cpuset.CPUSet
	podToCPUS  map[string]cpuset.CPUSet

	// TODO(li) Now initialized to cpumanager.TakeByTopology,
	// which is made public from cpumanager.takeByTopology() from this purpose,
	// need customization later and change it back to private then.
	//
	// typeTakeByTopology allocates CPUs associated with low-numbered cores from allCPUs.
	// For example: Given a system with 8 CPUs available and HT enabled,
	// if numReservedCPUs=2, then reserved={0,4}
	takeByTopologyFunc cpumanager.TypeTakeByTopologyFunc
}

var _ Cgroup = &cgroupCPUSet{}

// typeNewCgroupCPUSet is the type for cm.NewCgroupCPUSet below
type typeNewCgroupCPUSet func(cpuTopology *cputopology.CPUTopology,
	takeByTopologyFunc cpumanager.TypeTakeByTopologyFunc,
	cpusSpecific cpuset.CPUSet,
	nodeAllocatableReservation v1.ResourceList) (Cgroup, error)

// NewCgroupCPUSet creates cgroupCPUSet
func NewCgroupCPUSet(cpuTopology *cputopology.CPUTopology,
	takeByTopologyFunc cpumanager.TypeTakeByTopologyFunc,
	// For kubelet option "--reserved-cpus"
	cpusSpecific cpuset.CPUSet,
	// Used to calculate "total" - "--kube-reserved" - "--system-reserved"
	nodeAllocatableReservation v1.ResourceList) (Cgroup, error) {
	klog.Infof("[policymanager] Create cgroupCPUSet")

	reservedCPU, ok := nodeAllocatableReservation[v1.ResourceCPU]
	if !ok {
		return nil, fmt.Errorf("[policymanager] unable to determine reserved CPUs")
	}
	if reservedCPU.IsZero() {
		// TODO(li) Maybe we can do differently here than static policy below.
		// The static policy requires this to be nonzero. Zero CPU reservation
		// would allow the shared pool to be completely exhausted. At that point
		// either we would violate our guarantee of exclusivity or need to evict
		// any pod that has at least one container that requires zero CPUs.
		// See the comments in policy_static.go for more details.
		return nil, fmt.Errorf("[policymanager] requires systemreserved.cpu + kubereserved.cpu to be greater than zero")
	}
	reservedCPUsFloat := float64(reservedCPU.MilliValue()) / 1000
	// TODO(li) We may be able to use factional CPUs for cpusReserved in the future.
	numReservedCPUs := int(math.Ceil(reservedCPUsFloat))

	cpusReserved := cpuset.NewCPUSet()
	cpusAll := cpuTopology.CPUDetails.CPUs()
	if cpusSpecific.Size() > 0 {
		cpusReserved = cpusSpecific
	} else {
		// When failed, cpusReserved == cpuset.NewCPUSet(), which cause error below
		cpusReserved, _ = takeByTopologyFunc(cpuTopology, cpusAll, numReservedCPUs)
	}

	if cpusReserved.Size() != numReservedCPUs {
		return nil, fmt.Errorf("[policymanager] unable to reserve the required amount of CPUs (size of %s did not equal %d)",
			cpusReserved, numReservedCPUs)
	}
	klog.Infof("[policymanager] reserved %d CPUs (\"%s\") not available for exclusive assignment",
		cpusReserved.Size(), cpusReserved)

	ccs := &cgroupCPUSet{
		podSet:             sets.NewString(),
		cpusReserved:       cpusReserved,
		cpusShared:         cpusAll.Difference(cpusReserved),
		podToCPUS:          make(map[string]cpuset.CPUSet),
		cpuTopology:        cpuTopology,
		takeByTopologyFunc: takeByTopologyFunc,
	}

	return ccs, nil
}

// TODO(li) To enable crash recovery of kubelet,
// we need to move some initialization logic to Start()
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

	// Write cgroup values for this pod to host
	policy := getPodPolicy(pod)
	switch policy {
	case policyDefault, policyCPUCFS:
		klog.Infof("[policymanager] policy (%q) will use cpusShared pool",
			policy)
	case policyIsolated:
		if err := ccs.addPodUpdate(pod); err != nil {
			return err
		}
	default:
		return fmt.Errorf("policy (%q) not supported", policy)
	}

	return nil
}

// get numCPUS for cpuset.cpus of this pod
// TODO(li) Now we return request CPU, should we also return limit?
func (ccs *cgroupCPUSet) getNumCPUS(pod *v1.Pod) (cpusRequest int) {
	resourceRequest, _ := resource.PodRequestsAndLimits(pod)

	cpuQuantity, found := resourceRequest[v1.ResourceCPU]
	if !found {
		return 0
	}
	// fractional is rounded to its ceil
	return int(math.Ceil(float64(cpuQuantity.Value())))
}

// Update all podToValue (map[string(pod.UID)] -> value) for this pod
func (ccs *cgroupCPUSet) addPodUpdate(pod *v1.Pod) (rerr error) {
	cpusPodNum := ccs.getNumCPUS(pod)
	if cpusPodNum == 0 {
		return fmt.Errorf("skip pod (%q) that need 0 dedicated CPUs", pod.Name)
	}

	cpusPod, err := ccs.takeByTopologyFunc(ccs.cpuTopology, ccs.cpusShared,
		cpusPodNum)
	if err != nil {
		return err
	}

	ccs.podToCPUS[string(pod.UID)] = cpusPod
	ccs.cpusShared = ccs.cpusShared.Difference(cpusPod)

	klog.Infof("[policymanager] cgroupCPUSet (%+v) after addPodUpdate", ccs)

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

	// Return dedicated cpuset.cpus back to the shared CPU pool
	if cpusPod, found := ccs.podToCPUS[podUID]; found {
		ccs.cpusShared = ccs.cpusShared.Union(cpusPod)
		delete(ccs.podToCPUS, podUID)
	}

	klog.Infof("[policymanager] cgroupCPUSet (%+v) after RemovePod", ccs)

	return nil
}

func (ccs *cgroupCPUSet) UpdatePod(pod *v1.Pod) (rerr error) {
	return nil
}
