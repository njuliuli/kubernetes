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
	"k8s.io/kubernetes/pkg/api/v1/resource"
	v1qos "k8s.io/kubernetes/pkg/apis/core/v1/helper/qos"
)

const (
	// Share const (SharesPerCPU, MilliCPUToCPU) in pkg/kubelet/cm/helper_linux.go
	// for units conversion
	// 2ms
	cpuSharesMin = 2
	// 100000 -> 100ms
	cpuPeriodDefault uint64 = 100000
	// 1000 -> 1ms
	cpuQuotaMin = 1000
)

// getCPUShares calculate CFS CPU share from v1.ResourceList
func getCPUShares(resourceRequest v1.ResourceList) uint64 {
	cpuRequest := int64(0)
	if request, found := resourceRequest[v1.ResourceCPU]; found {
		cpuRequest = request.MilliValue()
	}

	if cpuRequest == 0 {
		// Docker converts zero milliCPU to unset, which maps to kernel default
		// for unset: 1024. Return 2 here to really match kernel default for
		// zero milliCPU.
		return cpuSharesMin
	}

	// Conceptually (milliCPU / MilliCPUToCPU) * SharesPerCPU, but factored to improve rounding.
	shares := (cpuRequest * SharesPerCPU) / MilliCPUToCPU
	if shares < cpuSharesMin {
		return cpuSharesMin
	}

	return uint64(shares)
}

// getCPUQuota calculate CFS CPU quota from v1.ResourceList and cpuPeriod in milliCPU
func getCPUQuota(resourceLimits v1.ResourceList, cpuPeriod int64) (cpuQuota int64) {
	cpuLimit := int64(0)
	if limit, found := resourceLimits[v1.ResourceCPU]; found {
		cpuLimit = limit.MilliValue()
	}

	// CFS quota is measured in two values:
	//  - cfs_period_us=100ms (the amount of time to measure usage across given by period)
	//  - cfs_quota=20ms (the amount of cpu time allowed to be used across a period)
	// so in the above example, you are limited to 20% of a single CPU
	// for multi-cpu environments, you just scale equivalent amounts
	// see https://www.kernel.org/doc/Documentation/scheduler/sched-bwc.txt for details
	if cpuLimit == 0 {
		return cpuQuota
	}

	// we then convert your milliCPU to a value normalized over a period
	cpuQuota = (cpuLimit * cpuPeriod) / MilliCPUToCPU

	// quota needs to be a minimum of 1ms.
	if cpuQuota < cpuQuotaMin {
		cpuQuota = cpuQuotaMin
	}
	return cpuQuota
}

// cgroupCPUCFS is used to manage all CFS related cgroup values,
// such as cpu.shares, cpu.cfs_period_us, and cpu.cfs_quota_us.
type cgroupCPUCFS struct {
	// Track all pods added to cgroupCPUCFS, set of pod.UID
	podSet sets.String

	// Below podToValue are of the form map[string(pod.UID)] -> value.
	// In ResourceConfig, fileds not set is default to Go's nil,
	// while here, value not set means the key pod.UID is not in the map.

	// CPU shares (relative weight vs. other containers).
	podToCPUShares map[string]uint64
	// CPU hardcap limit (in usecs). Allowed cpu time in a given period.
	podToCPUQuota map[string]int64
	// CPU quota period.
	podToCPUPeriod map[string]uint64
}

var _ Cgroup = &cgroupCPUCFS{}

// typeNewCgroupCPUCFS is the type for function cm.NewCgroupCPUCFS below
type typeNewCgroupCPUCFS func() (Cgroup, error)

// NewCgroupCPUCFS creates cgroupCPUCFS
func NewCgroupCPUCFS() (Cgroup, error) {
	klog.Infof("[policymanager] Create cgroupCPUCFS")

	ccc := &cgroupCPUCFS{
		podSet:         sets.NewString(),
		podToCPUShares: make(map[string]uint64),
		podToCPUQuota:  make(map[string]int64),
		podToCPUPeriod: make(map[string]uint64),
	}

	return ccc, nil
}

func (ccc *cgroupCPUCFS) Start() error {
	klog.Infof("[policymanager] Start cgroupCPUCFS, %+v", ccc)

	return nil
}

func (ccc *cgroupCPUCFS) AddPod(pod *v1.Pod) error {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}

	klog.Infof("[policymanager] Add pod (%q) to cgroupCPUCFS", pod.Name)

	// A pod can only be added once, update is not supported for now
	podUID := string(pod.UID)
	if ccc.podSet.Has(podUID) {
		return fmt.Errorf("pod (%q) already added to cgroupCPUCFS", pod.Name)
	}
	// TODO(li) Do we need to defer insertion only if "rerr != nil"?
	// Maybe not necessary, since the same pod will not be added twice.
	// Then maybe clean up of failed adding pod is needed
	// when anything before cgroup update failed?
	ccc.podSet.Insert(podUID)

	// Write cgroup values for this pod to host
	policy := getPodPolicy(pod)
	switch policy {
	case policyCPUCFS:
		if err := ccc.addPodUpdate(pod); err != nil {
			return err
		}
	case policyDefault, policyIsolated:
		klog.Infof("[policymanager] policy (%q), cgroupCPUCFS is not changed",
			policy)
	default:
		return fmt.Errorf("policy (%q) not supported", policy)
	}

	return nil
}

// Update all podToValue (map[string(pod.UID)] -> value) for this pod
func (ccc *cgroupCPUCFS) addPodUpdate(pod *v1.Pod) error {
	// compute cpuShares and cpuQuota
	podUID := string(pod.UID)
	resourceRequest, resourceLimits := resource.PodRequestsAndLimits(pod)
	cpuShares := getCPUShares(resourceRequest)
	cpuPeriod := cpuPeriodDefault
	cpuQuota := getCPUQuota(resourceLimits, int64(cpuPeriod))

	// TODO(li) I will replace it based on per-task policy
	// now still based on the qos class
	switch qosClass := v1qos.GetPodQOS(pod); qosClass {
	case v1.PodQOSGuaranteed:
		ccc.podToCPUShares[podUID] = cpuShares
		ccc.podToCPUPeriod[podUID] = cpuPeriod
		ccc.podToCPUQuota[podUID] = cpuQuota
	case v1.PodQOSBurstable:
		ccc.podToCPUShares[podUID] = cpuShares
		// if a container is not limited, the pod is not limited
		cpuLimitsDeclared := true
		for _, container := range pod.Spec.Containers {
			if container.Resources.Limits.Cpu().IsZero() {
				cpuLimitsDeclared = false
			}
		}
		if cpuLimitsDeclared {
			ccc.podToCPUPeriod[podUID] = cpuPeriod
			ccc.podToCPUQuota[podUID] = cpuQuota
		} else {
			// This cleanup should not be necessary, add just in case
			if _, found := ccc.podToCPUQuota[podUID]; found {
				delete(ccc.podToCPUQuota, podUID)
			}
			if _, found := ccc.podToCPUPeriod[podUID]; found {
				delete(ccc.podToCPUPeriod, podUID)
			}
		}
	case v1.PodQOSBestEffort:
		ccc.podToCPUShares[podUID] = cpuSharesMin
		// This cleanup should not be necessary, add just in case
		if _, found := ccc.podToCPUQuota[podUID]; found {
			delete(ccc.podToCPUQuota, podUID)
		}
		if _, found := ccc.podToCPUPeriod[podUID]; found {
			delete(ccc.podToCPUPeriod, podUID)
		}
	default:
		return fmt.Errorf("pod (%q) qosClass (%q) is unkonwn",
			pod.Name, qosClass)
	}

	klog.Infof("[policymanager] cgroupCPUCFS (%+v) after addPodUpdate", ccc)

	return nil
}

func (ccc *cgroupCPUCFS) RemovePod(podUID string) error {
	if !ccc.podSet.Has(podUID) {
		return fmt.Errorf("pod not added to cgroupCPUCFS yet")
	}
	ccc.podSet.Delete(podUID)

	// just remove everything about pod.UID
	if _, found := ccc.podToCPUShares[podUID]; found {
		delete(ccc.podToCPUShares, podUID)
	}
	if _, found := ccc.podToCPUQuota[podUID]; found {
		delete(ccc.podToCPUQuota, podUID)
	}
	if _, found := ccc.podToCPUPeriod[podUID]; found {
		delete(ccc.podToCPUPeriod, podUID)
	}

	klog.Infof("[policymanager] cgroupCPUCFS (%+v) after RemovePod", ccc)

	return nil
}

func (ccc *cgroupCPUCFS) ReadPod(pod *v1.Pod) (rc *ResourceConfig, isTracked bool) {
	// When the pod is just removed
	// TODO(li) Do we need to update cgroup values here, before deleting cgroup path?
	// Maybe set cpu.shares for this pod to cpuSharesMin here?
	// Now I think it is not necessary.
	rcDefault := &ResourceConfig{}

	if pod == nil {
		klog.Infof("[policymanager] ReadPod, pod not exist, should never happen!")
		return rcDefault, false
	}
	podUID := string(pod.UID)
	if !ccc.podSet.Has(podUID) {
		return rcDefault, false
	}

	// When the pod is just added
	rc = &ResourceConfig{}
	if cpuShares, found := ccc.podToCPUShares[podUID]; found {
		rc.CpuShares = &cpuShares
	}
	if cpuPeriod, found := ccc.podToCPUPeriod[podUID]; found {
		rc.CpuPeriod = &cpuPeriod
	}
	if cpuQuota, found := ccc.podToCPUQuota[podUID]; found {
		rc.CpuQuota = &cpuQuota
	}

	return rc, true
}
