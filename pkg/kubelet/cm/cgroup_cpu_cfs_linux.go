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

// Share const (SharesPerCPU, MilliCPUToCPU) in pkg/kubelet/cm/helper_linux.go
// for units conversion
const (
	// 2ms
	CPUSharesMin = 2
	// 100000 -> 100ms
	CPUPeriodDefault uint64 = 100000
	// 1000 -> 1ms
	CPUQuotaMin = 1000
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
		return CPUSharesMin
	}

	// Conceptually (milliCPU / MilliCPUToCPU) * SharesPerCPU, but factored to improve rounding.
	shares := (cpuRequest * SharesPerCPU) / MilliCPUToCPU
	if shares < CPUSharesMin {
		return CPUSharesMin
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
	if cpuQuota < CPUQuotaMin {
		cpuQuota = CPUQuotaMin
	}
	return cpuQuota
}

type cgroupCPUCFS struct {
	// Track all pods added to cgroupCPUCFS, set of pod.UID
	podSet sets.String

	// podToValue is of map[string(pod.UID)] -> value
	// CPU shares (relative weight vs. other containers).
	podToCPUShares map[string]uint64
	// CPU hardcap limit (in usecs). Allowed cpu time in a given period.
	podToCPUQuota map[string]int64
	// CPU quota period.
	podToCPUPeriod map[string]uint64
}

var _ Cgroup = &cgroupCPUCFS{}

// NewCgroupCPUCFS creates state for cpu.shares
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

func (ccc *cgroupCPUCFS) Start() (rerr error) {
	klog.Infof("[policymanager] Start cgroupCPUCFS, %+v", ccc)

	return nil
}

func (ccc *cgroupCPUCFS) AddPod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}

	klog.Infof("[policymanager] Add pod (Name = %q) to cgroupCPUCFS", pod.Name)

	// A pod can only be added once, update is not supported for now
	podUID := string(pod.UID)
	if ccc.podSet.Has(podUID) {
		return fmt.Errorf("pod (Name = %q) already added to cgroupCPUCFS", pod.Name)
	}
	ccc.podSet.Insert(podUID)

	// compute cpuShares and cpuQuota
	resourceRequest, resourceLimits := resource.PodRequestsAndLimits(pod)
	cpuShares := getCPUShares(resourceRequest)
	cpuPeriod := CPUPeriodDefault
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
		}
	case v1.PodQOSBestEffort:
		ccc.podToCPUShares[podUID] = CPUSharesMin
	default:
		return fmt.Errorf("pod (Name = %q) qosClass (%q) is unkonwn", pod.Name, qosClass)
	}

	klog.Infof("[policymanager] cgroupCPUCFS (%+v) after AddPod", ccc)

	return nil
}

func (ccc *cgroupCPUCFS) RemovePod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}

	klog.Infof("[policymanager] Remove pod from cgroupCPUCFS, %q", pod.Name)

	podUID := string(pod.UID)
	if !ccc.podSet.Has(podUID) {
		return fmt.Errorf("pod (Name = %q) not added to cgroupCPUCFS yet", pod.Name)
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
