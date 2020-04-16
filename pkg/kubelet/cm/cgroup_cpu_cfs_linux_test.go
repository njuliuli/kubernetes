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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

// testCgroupCPUCFS is used to generate cgroupCPUCFS using in test
type testCgroupCPUCFS struct {
	podSet         sets.String
	podToCPUShares map[string]uint64
	podToCPUQuota  map[string]int64
	podToCPUPeriod map[string]uint64
}

// Generate default cgroupCPUCFS
func testGenerateCgroupCPUCFS(tccc *testCgroupCPUCFS) *cgroupCPUCFS {
	// If pod state fileds (podSet, podToValue) are not set,
	// the default value of generated cgroupCPUCFS is set to empty instead of Go's nil
	podSet := sets.NewString()
	if tccc.podSet != nil {
		podSet = tccc.podSet
	}
	podToCPUShares := make(map[string]uint64)
	if tccc.podToCPUShares != nil {
		podToCPUShares = tccc.podToCPUShares
	}
	podToCPUQuota := make(map[string]int64)
	if tccc.podToCPUQuota != nil {
		podToCPUQuota = tccc.podToCPUQuota
	}
	podToCPUPeriod := make(map[string]uint64)
	if tccc.podToCPUPeriod != nil {
		podToCPUPeriod = tccc.podToCPUPeriod
	}

	return &cgroupCPUCFS{
		podSet:         podSet,
		podToCPUShares: podToCPUShares,
		podToCPUQuota:  podToCPUQuota,
		podToCPUPeriod: podToCPUPeriod,
	}
}

// Make a copy of uint64/int64 and return pointer to it.
// This is needed to fill ResourceConfig with fields' default value being nil.
func testCopyInt64(value int64) *int64 {
	return &value
}

func testCopyUint64(value uint64) *uint64 {
	return &value
}

// Check if the cgroup values in two cgroupCPUCFS equal
func testEqualCgroupCPUCFS(t *testing.T,
	expect *cgroupCPUCFS, actual *cgroupCPUCFS) {
	assert.Equal(t, expect.podSet, actual.podSet)
	assert.Equal(t, expect.podToCPUShares, actual.podToCPUShares)
	assert.Equal(t, expect.podToCPUQuota, actual.podToCPUQuota)
	assert.Equal(t, expect.podToCPUPeriod, actual.podToCPUPeriod)
}

// Generate pod with given fields set, with pod.Policy=policyCFS
func testGeneratePodWithUID(uid string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: types.UID(uid),
		},
	}
}

// Since NewCgroupCPUCFS(...) and CgroupCPUCFS.Start() are executed together in this order,
// we test them together.
func TestNewCgroupCPUCFSAndStart(t *testing.T) {
	ccc, err := NewCgroupCPUCFS()
	if err == nil {
		err = ccc.Start()
	}

	assert.Nil(t, err)
	testEqualCgroupCPUCFS(t, testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
		ccc.(*cgroupCPUCFS))
}

// Now we only handle simple cases.
// Cases not test:
// (1) invalid pod with a container with request > limit (not empty),
// which should be validated by protobuf.
// (2) some corner cases depends on const like cpuSharesMin,
// for example, cpuRequest -> cpuShares < cpuSharesMin
// (3) TODO(li) pod with multiple containers,
// which may need different handling than qosClass in current kubernetes
func TestCgroupCPUCFSAddPod(t *testing.T) {
	cpuSmall := "100m"
	cpuSmallShare := uint64(102)
	cpuSmallQuota := int64(10000)
	cpuLarge := "200m"
	cpuLargeQuota := int64(20000)

	// Some fields are skipped for simplicity,
	// such as pod and expErr, are default to Go's nil in check below
	testCaseArray := []struct {
		description string
		cccBefore   *cgroupCPUCFS
		pod         *v1.Pod
		cccAfter    *cgroupCPUCFS
		expErr      error
	}{
		{
			description: "Fail, pod not existed",
			cccBefore:   testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cccAfter:    testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description: "Fail, pod already added",
			cccBefore: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet: sets.NewString("1"),
			}),
			pod: testGeneratePod(policyCPUCFS, "1", "", ""),
			cccAfter: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet: sets.NewString("1"),
			}),
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: "Fail, policy unknown",
			cccBefore:   testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			pod:         testGeneratePod(policyUnknown, "1", "", ""),
			cccAfter: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet: sets.NewString("1"),
			}),
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: "Success, request == limit",
			cccBefore:   testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			pod:         testGeneratePod(policyCPUCFS, "1", cpuSmall, cpuSmall),
			cccAfter: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": cpuSmallShare},
				podToCPUQuota:  map[string]int64{"1": cpuSmallQuota},
				podToCPUPeriod: map[string]uint64{"1": cpuPeriodDefault},
			}),
		},
		{
			description: "Success, request < limit",
			cccBefore:   testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			pod:         testGeneratePod(policyCPUCFS, "1", cpuSmall, cpuLarge),
			cccAfter: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": cpuSmallShare},
				podToCPUQuota:  map[string]int64{"1": cpuLargeQuota},
				podToCPUPeriod: map[string]uint64{"1": cpuPeriodDefault},
			}),
		},
		{
			description: "Success, request (empty), limit (not empty)",
			cccBefore:   testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			pod:         testGeneratePod(policyCPUCFS, "1", "", cpuLarge),
			cccAfter: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": cpuSharesMin},
				podToCPUQuota:  map[string]int64{"1": cpuLargeQuota},
				podToCPUPeriod: map[string]uint64{"1": cpuPeriodDefault},
			}),
		},
		{
			description: "Success, request (not empty), limit (empty)",
			cccBefore:   testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			pod:         testGeneratePod(policyCPUCFS, "1", cpuSmall, ""),
			cccAfter: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": cpuSmallShare},
			}),
		},
		{
			description: "Success, request (empty) == limit (empty)",
			cccBefore:   testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			pod:         testGeneratePod(policyCPUCFS, "1", "", ""),
			cccAfter: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": cpuSharesMin},
			}),
		},
		{
			description: "Success, request == limit, with some existing pods",
			cccBefore: &cgroupCPUCFS{
				podSet: sets.NewString("2", "3"),
				podToCPUShares: map[string]uint64{
					"2": cpuSmallShare * 2,
					"3": cpuSmallShare * 3,
				},
				podToCPUQuota: map[string]int64{
					"2": cpuSmallQuota * 2,
					"3": cpuSmallQuota * 3,
				},
				podToCPUPeriod: map[string]uint64{
					"2": cpuPeriodDefault * 2,
					"3": cpuPeriodDefault * 3,
				},
			},
			pod: testGeneratePod(policyCPUCFS, "1", cpuSmall, cpuSmall),
			cccAfter: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet: sets.NewString("1", "2", "3"),
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
					"1": cpuPeriodDefault,
					"2": cpuPeriodDefault * 2,
					"3": cpuPeriodDefault * 3,
				},
			}),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			ccc := tc.cccBefore
			err := ccc.AddPod(tc.pod)

			testEqualCgroupCPUCFS(t, tc.cccAfter, ccc)
			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestCgroupCPUCFSRemovePod(t *testing.T) {
	testCaseArray := []struct {
		description string
		cccBefore   *cgroupCPUCFS
		podUID      string
		cccAfter    *cgroupCPUCFS
		expErr      error
	}{
		{
			description: "Fail, pod not existed",
			cccBefore:   testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			expErr:      fmt.Errorf("fake error"),
			cccAfter:    testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
		},
		{
			description: "Fail, pod not added yet",
			cccBefore:   testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			podUID:      "1",
			expErr:      fmt.Errorf("fake error"),
			cccAfter:    testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
		},
		{
			description: "Success, one existing pod, with request < limit",
			cccBefore: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": 1},
			}),
			podUID:   "1",
			cccAfter: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
		},
		{
			description: "Success, an existing pod, with request = limit",
			cccBefore: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": 1},
				podToCPUQuota:  map[string]int64{"1": 1},
				podToCPUPeriod: map[string]uint64{"1": 1},
			}),
			podUID:   "1",
			cccAfter: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
		},
		{
			description: "Success, multiple existing pods",
			cccBefore: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet: sets.NewString("1", "2", "3"),
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
			}),
			podUID: "1",
			cccAfter: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet: sets.NewString("2", "3"),
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
			}),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			ccc := tc.cccBefore

			err := ccc.RemovePod(string(tc.podUID))

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			testEqualCgroupCPUCFS(t, tc.cccAfter, ccc)
		})
	}
}

func TestCgroupCPUCFSReadPod(t *testing.T) {
	type testCaseStruct struct {
		description  string
		ccc          *cgroupCPUCFS
		pod          *v1.Pod
		expRC        *ResourceConfig
		expIsTracked bool
	}
	var testCaseArray []testCaseStruct

	// One existing pod
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: "Pod in 3 maps",
			ccc: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": 11},
				podToCPUQuota:  map[string]int64{"1": 12},
				podToCPUPeriod: map[string]uint64{"1": 13},
			}),
			pod: testGeneratePodWithUID("1"),
			expRC: &ResourceConfig{
				CpuShares: testCopyUint64(11),
				CpuQuota:  testCopyInt64(12),
				CpuPeriod: testCopyUint64(13),
			},
			expIsTracked: true,
		},
		{
			description: "Pod in 2 maps",
			ccc: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": 11},
				podToCPUQuota:  map[string]int64{"1": 12},
			}),
			pod: testGeneratePodWithUID("1"),
			expRC: &ResourceConfig{
				CpuShares: testCopyUint64(11),
				CpuQuota:  testCopyInt64(12),
			},
			expIsTracked: true,
		},
		{
			description: "Pod in 1 maps",
			ccc: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet:         sets.NewString("1"),
				podToCPUShares: map[string]uint64{"1": 11},
			}),
			pod: testGeneratePodWithUID("1"),
			expRC: &ResourceConfig{
				CpuShares: testCopyUint64(11),
			},
			expIsTracked: true,
		},
		{
			description: "Pod in 0 map",
			ccc: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet: sets.NewString("1"),
			}),
			pod:          testGeneratePodWithUID("1"),
			expRC:        &ResourceConfig{},
			expIsTracked: true,
		},
		{
			description:  "Pod not in podSet",
			ccc:          testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			pod:          testGeneratePodWithUID("1"),
			expRC:        &ResourceConfig{},
			expIsTracked: false,
		},
	}...)

	// Multiple existing pods of different kinds
	cccFake := testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
		podSet: sets.NewString("1", "2", "3"),
		podToCPUShares: map[string]uint64{
			"1": 11,
			"2": 21,
		},
		podToCPUQuota: map[string]int64{
			"1": 12,
		},
		podToCPUPeriod: map[string]uint64{
			"1": 13,
		},
	})
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: "Multiple pods, target pod in 3 maps",
			ccc:         cccFake,
			pod:         testGeneratePodWithUID("1"),
			expRC: &ResourceConfig{
				CpuShares: testCopyUint64(11),
				CpuQuota:  testCopyInt64(12),
				CpuPeriod: testCopyUint64(13),
			},
			expIsTracked: true,
		},
		{
			description: "Multiple pods, target pod in 1 maps",
			ccc:         cccFake,
			pod:         testGeneratePodWithUID("2"),
			expRC: &ResourceConfig{
				CpuShares: testCopyUint64(21),
			},
			expIsTracked: true,
		},
		{
			description:  "Multiple pods, target pod in 0 maps",
			ccc:          cccFake,
			pod:          testGeneratePodWithUID("3"),
			expRC:        &ResourceConfig{},
			expIsTracked: true,
		},
		{
			description:  "Multiple pods, target pod not in podSet",
			ccc:          cccFake,
			pod:          testGeneratePodWithUID("4"),
			expRC:        &ResourceConfig{},
			expIsTracked: false,
		},
	}...)

	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description:  "Pod = nil, should never happen",
			ccc:          cccFake,
			expRC:        &ResourceConfig{},
			expIsTracked: false,
		},
	}...)

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			resourceConfig, isTracked := tc.ccc.ReadPod(tc.pod)

			assert.Equal(t, tc.expIsTracked, isTracked)
			assert.Equal(t, tc.expRC, resourceConfig)
		})
	}
}
