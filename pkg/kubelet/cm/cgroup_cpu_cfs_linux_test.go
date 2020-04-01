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
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Generate default cgroupCPUCFS
func generateEmptyCgroupCPUCFS() *cgroupCPUCFS {
	return &cgroupCPUCFS{
		podSet:         sets.NewString(),
		podToCPUShares: make(map[string]uint64),
		podToCPUQuota:  make(map[string]int64),
		podToCPUPeriod: make(map[string]uint64),
	}
}

// Generate pod with given fields set
func generatePod(uid, cpuRequest, cpuLimit string) *v1.Pod {
	rr := v1.ResourceRequirements{}
	if cpuRequest != "" {
		rr.Requests = v1.ResourceList{
			v1.ResourceCPU: resource.MustParse(cpuRequest),
		}
	}
	if cpuLimit != "" {
		rr.Limits = v1.ResourceList{
			v1.ResourceCPU: resource.MustParse(cpuLimit),
		}
	}

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: types.UID(uid),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Resources: rr,
				},
			},
		},
	}
}

func TestNewCgroupCPUCFS(t *testing.T) {
	_, err := NewCgroupCPUCFS()

	assert.Nil(t, err, "Creating cgroupCPUCFS failed")
}

func TestCgroupCPUCFSStart(t *testing.T) {
	ccc, _ := NewCgroupCPUCFS()

	err := ccc.Start()

	assert.Nil(t, err, "Starting cgroupCPUCFS failed")
}

// Now we only handle simple cases.
// Cases not test:
// (1) invalid pod with a container with request > limit (not empty),
// which should be validated by protobuf.
// (2) some corner cases depends on const like CPUSharesMin,
// for example, cpuRequest -> cpuShares < CPUSharesMin
// (3) TODO(li) pod with multiple containers,
// which may need different handling than qosClass in current kubernetes
func TestCgroupCPUCFSAddPod(t *testing.T) {
	cpuSmall := "100m"
	cpuSmallShare := uint64(102)
	cpuSmallQuota := int64(10000)
	cpuLarge := "200m"
	cpuLargeQuota := int64(20000)

	testCaseArray := []struct {
		description string
		cccBefore   *cgroupCPUCFS
		pod         *v1.Pod
		cccAfter    *cgroupCPUCFS
		expErr      error
	}{
		{
			description: "Fail, pod not existed",
			cccBefore:   generateEmptyCgroupCPUCFS(),
			pod:         nil,
			expErr:      fmt.Errorf("fake error"),
			cccAfter:    generateEmptyCgroupCPUCFS(),
		},
		{
			description: "Fail, pod already added",
			cccBefore: &cgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: make(map[string]uint64),
				podToCPUQuota:  make(map[string]int64),
				podToCPUPeriod: make(map[string]uint64),
			},
			pod:    generatePod("1", "", ""),
			expErr: fmt.Errorf("fake error"),
			cccAfter: &cgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: make(map[string]uint64),
				podToCPUQuota:  make(map[string]int64),
				podToCPUPeriod: make(map[string]uint64),
			},
		},
		{
			description: "Success, request == limit",
			cccBefore:   generateEmptyCgroupCPUCFS(),
			pod:         generatePod("1", cpuSmall, cpuSmall),
			expErr:      nil,
			cccAfter: &cgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": cpuSmallShare},
				podToCPUQuota:  map[string]int64{"1": cpuSmallQuota},
				podToCPUPeriod: map[string]uint64{"1": CPUPeriodDefault},
			},
		},
		{
			description: "Success, request < limit",
			cccBefore:   generateEmptyCgroupCPUCFS(),
			pod:         generatePod("1", cpuSmall, cpuLarge),
			expErr:      nil,
			cccAfter: &cgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": cpuSmallShare},
				podToCPUQuota:  map[string]int64{"1": cpuLargeQuota},
				podToCPUPeriod: map[string]uint64{"1": CPUPeriodDefault},
			},
		},
		{
			description: "Success, request (empty), limit (not empty)",
			cccBefore:   generateEmptyCgroupCPUCFS(),
			pod:         generatePod("1", "", cpuLarge),
			expErr:      nil,
			cccAfter: &cgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": CPUSharesMin},
				podToCPUQuota:  map[string]int64{"1": cpuLargeQuota},
				podToCPUPeriod: map[string]uint64{"1": CPUPeriodDefault},
			},
		},
		{
			description: "Success, request (not empty), limit (empty)",
			cccBefore:   generateEmptyCgroupCPUCFS(),
			pod:         generatePod("1", cpuSmall, ""),
			expErr:      nil,
			cccAfter: &cgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": cpuSmallShare},
				podToCPUQuota:  make(map[string]int64),
				podToCPUPeriod: make(map[string]uint64),
			},
		},
		{
			description: "Success, request (empty) == limit (empty)",
			cccBefore:   generateEmptyCgroupCPUCFS(),
			pod:         generatePod("1", "", ""),
			expErr:      nil,
			cccAfter: &cgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": CPUSharesMin},
				podToCPUQuota:  make(map[string]int64),
				podToCPUPeriod: make(map[string]uint64),
			},
		},
		{
			description: "Success, request == limit, with some existing pods",
			cccBefore: &cgroupCPUCFS{
				podSet: sets.NewString(),
				podToCPUShares: map[string]uint64{
					"2": cpuSmallShare * 2,
					"3": cpuSmallShare * 3,
				},
				podToCPUQuota: map[string]int64{
					"2": cpuSmallQuota * 2,
					"3": cpuSmallQuota * 3,
				},
				podToCPUPeriod: map[string]uint64{
					"2": CPUPeriodDefault * 2,
					"3": CPUPeriodDefault * 3,
				},
			},
			pod:    generatePod("1", cpuSmall, cpuSmall),
			expErr: nil,
			cccAfter: &cgroupCPUCFS{
				podSet: sets.NewString("1"),
				podToCPUShares: map[string]uint64{
					"1": cpuSmallShare,
					"2": cpuSmallShare * 2,
					"3": cpuSmallShare * 3,
				},
				podToCPUQuota: map[string]int64{
					"1": cpuSmallQuota,
					"2": cpuSmallQuota * 2,
					"3": cpuSmallQuota * 3,
				},
				podToCPUPeriod: map[string]uint64{
					"1": CPUPeriodDefault,
					"2": CPUPeriodDefault * 2,
					"3": CPUPeriodDefault * 3,
				},
			},
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			ccc := tc.cccBefore

			err := ccc.AddPod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, tc.cccAfter, ccc)
		})
	}
}

func TestCgroupCPUCFSRemovePod(t *testing.T) {
	testCaseArray := []struct {
		description string
		cccBefore   *cgroupCPUCFS
		pod         *v1.Pod
		cccAfter    *cgroupCPUCFS
		expErr      error
	}{
		{
			description: "Fail, pod not existed",
			cccBefore: &cgroupCPUCFS{
				podSet: sets.NewString(),
			},
			pod:    nil,
			expErr: fmt.Errorf("fake error"),
			cccAfter: &cgroupCPUCFS{
				podSet: sets.NewString(),
			},
		},
		{
			description: "Fail, pod not added yet",
			cccBefore: &cgroupCPUCFS{
				podSet: sets.NewString(),
			},
			pod:    generatePod("1", "", ""),
			expErr: fmt.Errorf("fake error"),
			cccAfter: &cgroupCPUCFS{
				podSet: sets.NewString(),
			},
		},
		{
			description: "Success, one existing pod, with request < limit",
			cccBefore: &cgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": 1},
				podToCPUQuota:  make(map[string]int64),
				podToCPUPeriod: make(map[string]uint64),
			},
			pod:      generatePod("1", "", ""),
			expErr:   nil,
			cccAfter: generateEmptyCgroupCPUCFS(),
		},
		{
			description: "Success, an existing pod, with request = limit",
			cccBefore: &cgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": 1},
				podToCPUQuota:  map[string]int64{"1": 1},
				podToCPUPeriod: map[string]uint64{"1": 1},
			},
			pod:      generatePod("1", "", ""),
			expErr:   nil,
			cccAfter: generateEmptyCgroupCPUCFS(),
		},
		{
			description: "Success, multiple existing pods",
			cccBefore: &cgroupCPUCFS{
				podSet: sets.NewString("1"),
				podToCPUShares: map[string]uint64{
					"1": 1,
					"2": 2,
					"3": 3,
				},
				podToCPUQuota: map[string]int64{
					"1": 1,
					"2": 2,
					"3": 3,
				},
				podToCPUPeriod: map[string]uint64{
					"1": 1,
					"2": 2,
					"3": 3,
				},
			},
			pod:    generatePod("1", "100m", "200m"),
			expErr: nil,
			cccAfter: &cgroupCPUCFS{
				podSet: sets.NewString(),
				podToCPUShares: map[string]uint64{
					"2": 2,
					"3": 3,
				},
				podToCPUQuota: map[string]int64{
					"2": 2,
					"3": 3,
				},
				podToCPUPeriod: map[string]uint64{
					"2": 2,
					"3": 3,
				},
			},
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			ccc := tc.cccBefore

			err := ccc.RemovePod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, tc.cccAfter, ccc)
		})
	}
}
