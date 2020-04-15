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
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

var (
	// Use the real CPU topology of my physical host
	testTopologyDualSocketHT = &topology.CPUTopology{
		NumCPUs:    24,
		NumSockets: 2,
		NumCores:   12,
		CPUDetails: map[int]topology.CPUInfo{
			0:  {CoreID: 0, SocketID: 0, NUMANodeID: 0},
			1:  {CoreID: 1, SocketID: 1, NUMANodeID: 1},
			2:  {CoreID: 2, SocketID: 0, NUMANodeID: 0},
			3:  {CoreID: 3, SocketID: 1, NUMANodeID: 1},
			4:  {CoreID: 4, SocketID: 0, NUMANodeID: 0},
			5:  {CoreID: 5, SocketID: 1, NUMANodeID: 1},
			6:  {CoreID: 6, SocketID: 0, NUMANodeID: 0},
			7:  {CoreID: 7, SocketID: 1, NUMANodeID: 1},
			8:  {CoreID: 8, SocketID: 0, NUMANodeID: 0},
			9:  {CoreID: 9, SocketID: 1, NUMANodeID: 1},
			10: {CoreID: 10, SocketID: 0, NUMANodeID: 0},
			11: {CoreID: 11, SocketID: 1, NUMANodeID: 1},
			12: {CoreID: 0, SocketID: 0, NUMANodeID: 0},
			13: {CoreID: 1, SocketID: 1, NUMANodeID: 1},
			14: {CoreID: 2, SocketID: 0, NUMANodeID: 0},
			15: {CoreID: 3, SocketID: 1, NUMANodeID: 1},
			16: {CoreID: 4, SocketID: 0, NUMANodeID: 0},
			17: {CoreID: 5, SocketID: 1, NUMANodeID: 1},
			18: {CoreID: 6, SocketID: 0, NUMANodeID: 0},
			19: {CoreID: 7, SocketID: 1, NUMANodeID: 1},
			20: {CoreID: 8, SocketID: 0, NUMANodeID: 0},
			21: {CoreID: 9, SocketID: 1, NUMANodeID: 1},
			22: {CoreID: 10, SocketID: 0, NUMANodeID: 0},
			23: {CoreID: 11, SocketID: 1, NUMANodeID: 1},
		},
	}
	// Reserve 4 CPUs for testTopologyDualSocketHT
	testNodeAllocatableReservationSuccess = v1.ResourceList{
		v1.ResourceCPU: *resource.NewQuantity(4, resource.DecimalSI),
	}
	testCPUSReserved = cpuset.NewCPUSet(0, 2, 12, 14)
	testCPUSShared   = testTopologyDualSocketHT.CPUDetails.CPUs().
				Difference(testCPUSReserved)
)

// Generate pod with given fields set, with pod.Policy=policyCFS
func testGeneratePodCPUSet(uid, cpuRequest, cpuLimit string) *v1.Pod {
	return testGeneratePod(policyIsolated, uid, cpuRequest, cpuLimit)
}

// testCgroupCPUSet is used to generate cgroupCPUSet using in test
type testCgroupCPUSet struct {
	podSet             sets.String
	cpuTopology        *topology.CPUTopology
	cpusReserved       cpuset.CPUSet
	cpusShared         cpuset.CPUSet
	podToCPUS          map[string]cpuset.CPUSet
	takeByTopologyFunc cpumanager.TypeTakeByTopologyFunc
}

// Generate default cgroupCPUSet, with customized default values after initialization
func testGenerateCgroupCPUSet(tccs *testCgroupCPUSet) *cgroupCPUSet {
	ccs := &cgroupCPUSet{
		podSet:             sets.NewString(),
		cpuTopology:        testTopologyDualSocketHT,
		cpusReserved:       testCPUSReserved,
		podToCPUS:          make(map[string]cpuset.CPUSet),
		takeByTopologyFunc: cpumanager.TakeByTopology,
	}

	// If not set, some fields are set to customized default value
	tccsDefault := &testCgroupCPUSet{}
	if !reflect.DeepEqual(tccs.podSet, tccsDefault.podSet) {
		ccs.podSet = tccs.podSet
	}
	if !reflect.DeepEqual(tccs.cpuTopology, tccsDefault.cpuTopology) {
		ccs.cpuTopology = tccs.cpuTopology
	}
	if !reflect.DeepEqual(tccs.cpusReserved, tccsDefault.cpusReserved) {
		ccs.cpusReserved = tccs.cpusReserved
	}
	ccs.cpusShared = ccs.cpuTopology.CPUDetails.CPUs().
		Difference(ccs.cpusReserved)
	if !reflect.DeepEqual(tccs.cpusShared, tccsDefault.cpusShared) {
		ccs.cpusShared = tccs.cpusShared
	}
	if !reflect.DeepEqual(tccs.podToCPUS, tccsDefault.podToCPUS) {
		ccs.podToCPUS = tccs.podToCPUS
	}
	if !reflect.DeepEqual(tccs.takeByTopologyFunc, tccsDefault.takeByTopologyFunc) {
		ccs.takeByTopologyFunc = tccs.takeByTopologyFunc
	}

	return ccs
}

// Check if the cgroup values in two cgroupCPUSet equal
func testEqualCgroupCPUSet(t *testing.T,
	expect *cgroupCPUSet, actual *cgroupCPUSet) {
	assert.Equal(t, expect.podSet, actual.podSet)
	assert.Equal(t, expect.cpusReserved, actual.cpusReserved)
	assert.Equal(t, expect.cpusShared, actual.cpusShared)
	assert.Equal(t, expect.podToCPUS, actual.podToCPUS)
}

func TestNewCgroupCPUSetAndStart(t *testing.T) {
	// When success, number of reserved CPUs is 2
	cpuTopologyFake := testTopologyDualSocketHT
	cpusAllFake := cpuTopologyFake.CPUDetails.CPUs()
	nodeAllocatableReservationSuccess := testNodeAllocatableReservationSuccess
	nodeAllocatableReservationFail := v1.ResourceList{
		v1.ResourceCPU: *resource.NewQuantity(0, resource.DecimalSI),
	}
	cpusReservedSuccess := testCPUSReserved
	cpusReservedFail := cpuset.NewCPUSet()
	cpusSpecificSuccess := cpuset.NewCPUSet(0, 1, 2, 3)
	cpusSpecificFail := cpuset.NewCPUSet()

	testCaseArray := []struct {
		description                string
		cpusSpecific               cpuset.CPUSet
		nodeAllocatableReservation v1.ResourceList
		cpusReserved               cpuset.CPUSet
		cpusReservedExp            cpuset.CPUSet
		errDiscover                error
		errTopology                error
		expErr                     error
	}{
		{
			description: "Fail, error from topologyDiscoverFunc",
			errDiscover: fmt.Errorf("fake error"),
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description: "Fail, error from takeByTopologyFunc",
			errTopology: fmt.Errorf("fake error"),
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description:                "Fail, none of them set: --reserved-cpus, --kube-reserved and --system-reserved",
			nodeAllocatableReservation: nodeAllocatableReservationFail,
			expErr:                     fmt.Errorf("fake error"),
		},
		{
			description:                "Fail, both cpusReserved and cpusSpecific fail",
			nodeAllocatableReservation: nodeAllocatableReservationFail,
			cpusReserved:               cpusReservedFail,
			cpusSpecific:               cpusSpecificFail,
			expErr:                     fmt.Errorf("fake error"),
		},
		{
			description:                "Success, get cpuReserved based on --kube-reserved and --system-reserved",
			nodeAllocatableReservation: nodeAllocatableReservationSuccess,
			cpusReserved:               cpusReservedSuccess,
			cpusReservedExp:            cpusReservedSuccess,
		},
		{
			description:                "Success, get cpuReserved based on --reserved-cpus",
			nodeAllocatableReservation: nodeAllocatableReservationSuccess,
			cpusSpecific:               cpusSpecificSuccess,
			cpusReservedExp:            cpusSpecificSuccess,
		},
		{
			description:                "Success, get cpuReserved based on --reserved-cpus, ignoring --kube-reserved and --system-reserved",
			nodeAllocatableReservation: nodeAllocatableReservationSuccess,
			cpusSpecific:               cpusSpecificSuccess,
			cpusReserved:               cpusReservedSuccess,
			cpusReservedExp:            cpusSpecificSuccess,
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			takeByTopologyFunc := func(topo *topology.CPUTopology,
				availableCPUs cpuset.CPUSet, numCPUs int) (cpuset.CPUSet, error) {
				if tc.errTopology == nil {
					return tc.cpusReserved, nil
				}
				return cpuset.NewCPUSet(), tc.errTopology
			}
			ccsExp := testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet:             sets.NewString(),
				cpuTopology:        cpuTopologyFake,
				cpusReserved:       tc.cpusReservedExp,
				cpusShared:         cpusAllFake.Difference(tc.cpusReservedExp),
				podToCPUS:          make(map[string]cpuset.CPUSet),
				takeByTopologyFunc: takeByTopologyFunc,
			})

			ccs, err := NewCgroupCPUSet(cpuTopologyFake, takeByTopologyFunc,
				tc.cpusSpecific, tc.nodeAllocatableReservation)
			if err == nil {
				err = ccs.Start()
			}

			if tc.expErr == nil {
				assert.Nil(t, err)
				testEqualCgroupCPUSet(t, ccsExp, ccs.(*cgroupCPUSet))
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestCgroupCPUSetAddPod(t *testing.T) {
	testCaseArray := []struct {
		description string
		ccsBefore   *cgroupCPUSet
		pod         *v1.Pod
		ccsAfter    *cgroupCPUSet
		expErr      error
	}{
		{
			description: "Fail, pod not existed",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			ccsAfter:    testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description: "Fail, pod already added",
			ccsBefore: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
			}),
			pod: testGeneratePodCPUSet("1", "", ""),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
			}),
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: "Fail, policy unknown",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			pod:         testGeneratePod(policyUnknown, "1", "", ""),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
			}),
			expErr: fmt.Errorf("fake error"),
		},
		// For some policies, the pod is tracked, but cgroupCPUSet is not changed
		{
			description: "Success, policyDefault, state unchanged",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			pod:         testGeneratePod(policyDefault, "1", "", ""),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
			}),
		},
		{
			description: "Success, policyCPUCFS, state unchanged",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			pod:         testGeneratePod(policyCPUCFS, "1", "100", "100"),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
			}),
		},
		// For policyIsolated, quantity in pod matters.
		// Tests below is based on default cgroupCPUSet generated by testGenerateCgroupCPUSet above
		{
			description: "Fail, policyIsolated, but pod needs 0 dedicated CPUs (request=limit, empty)",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			pod:         testGeneratePod(policyIsolated, "1", "", ""),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
			}),
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: "Fail, policyIsolated, pod is too large to fit into this host",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			pod:         testGeneratePod(policyIsolated, "1", "100", "100"),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
			}),
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: "Success, policyIsolated, pod with (request=limit, int, thread x1)",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			pod:         testGeneratePod(policyIsolated, "1", "1", "1"),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
				cpusShared: testCPUSShared.
					Difference(cpuset.NewCPUSet(1)),
				podToCPUS: map[string]cpuset.CPUSet{
					"1": cpuset.NewCPUSet(1),
				},
			}),
		},
		{
			description: "Success, policyIsolated, pod with (request=limit, int, thread x2 -> core x1)",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			pod:         testGeneratePod(policyIsolated, "1", "2", "2"),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
				cpusShared: testCPUSShared.
					Difference(cpuset.NewCPUSet(4, 16)),
				podToCPUS: map[string]cpuset.CPUSet{
					"1": cpuset.NewCPUSet(4, 16),
				},
			}),
		},
		{
			description: "Success, policyIsolated, pod with (request=limit, int, thread x12 -> node x1)",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			pod:         testGeneratePod(policyIsolated, "1", "12", "12"),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
				cpusShared: testCPUSShared.
					Difference(cpuset.NewCPUSet(1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23)),
				podToCPUS: map[string]cpuset.CPUSet{
					"1": cpuset.NewCPUSet(1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23),
				},
			}),
		},
		{
			description: "Success, multiple existing pods, policyIsolated, pod with (request=limit, int, thread x1)",
			ccsBefore: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("2", "3"),
				cpusShared: testCPUSShared.
					Difference(cpuset.NewCPUSet(1, 3, 4, 5, 7, 9, 11, 13, 15, 16, 17, 19, 21, 23)),
				podToCPUS: map[string]cpuset.CPUSet{
					"2": cpuset.NewCPUSet(4, 16),
					"3": cpuset.NewCPUSet(1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23),
				},
			}),
			pod: testGeneratePod(policyIsolated, "1", "1", "1"),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1", "2", "3"),
				cpusShared: testCPUSShared.
					Difference(cpuset.NewCPUSet(1, 3, 4, 5, 6, 7, 9, 11, 13, 15, 16, 17, 19, 21, 23)),
				podToCPUS: map[string]cpuset.CPUSet{
					"1": cpuset.NewCPUSet(6),
					"2": cpuset.NewCPUSet(4, 16),
					"3": cpuset.NewCPUSet(1, 3, 5, 7, 9, 11, 13, 15, 17, 19, 21, 23),
				},
			}),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			ccs := tc.ccsBefore

			err := ccs.AddPod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			testEqualCgroupCPUSet(t, tc.ccsAfter, ccs)
		})
	}
}

func TestCgroupCPUSetRemovePod(t *testing.T) {
	testCaseArray := []struct {
		description string
		ccsBefore   *cgroupCPUSet
		podUID      string
		ccsAfter    *cgroupCPUSet
		expErr      error
	}{
		{
			description: "Fail, pod not existed",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			ccsAfter:    testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description: "Fail, pod not added yet",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			podUID:      "1",
			ccsAfter:    testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			expErr:      fmt.Errorf("fake error"),
		},
		// Tests below is based on default cgroupCPUSet generated by testGenerateCgroupCPUSet above
		{
			description: "Success, one existing pod, without dedicated CPUs",
			ccsBefore: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
			}),
			podUID:   "1",
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		},
		{
			description: "Success, one existing pod, with dedicated CPUs",
			ccsBefore: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
				cpusShared: testCPUSShared.
					Difference(cpuset.NewCPUSet(1)),
				podToCPUS: map[string]cpuset.CPUSet{
					"1": cpuset.NewCPUSet(1),
				},
			}),
			podUID:   "1",
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		},
		{
			description: "Success, multiple existing pods, remove pod with dedicated CPUs",
			ccsBefore: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1", "2", "3"),
				cpusShared: testCPUSShared.
					Difference(cpuset.NewCPUSet(1, 4, 16)),
				podToCPUS: map[string]cpuset.CPUSet{
					"1": cpuset.NewCPUSet(1),
					"2": cpuset.NewCPUSet(4, 16),
				},
			}),
			podUID: "2",
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1", "3"),
				cpusShared: testCPUSShared.
					Difference(cpuset.NewCPUSet(1)),
				podToCPUS: map[string]cpuset.CPUSet{
					"1": cpuset.NewCPUSet(1),
				},
			}),
		},
		{
			description: "Success, multiple existing pods, remove pod without dedicated CPUs",
			ccsBefore: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1", "2", "3"),
				cpusShared: testCPUSShared.
					Difference(cpuset.NewCPUSet(1, 4, 16)),
				podToCPUS: map[string]cpuset.CPUSet{
					"1": cpuset.NewCPUSet(1),
					"2": cpuset.NewCPUSet(4, 16),
				},
			}),
			podUID: "3",
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1", "2"),
				cpusShared: testCPUSShared.
					Difference(cpuset.NewCPUSet(1, 4, 16)),
				podToCPUS: map[string]cpuset.CPUSet{
					"1": cpuset.NewCPUSet(1),
					"2": cpuset.NewCPUSet(4, 16),
				},
			}),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			ccs := tc.ccsBefore

			err := ccs.RemovePod(tc.podUID)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			testEqualCgroupCPUSet(t, tc.ccsAfter, ccs)
		})
	}
}
