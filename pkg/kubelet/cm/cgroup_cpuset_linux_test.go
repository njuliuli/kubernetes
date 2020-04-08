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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

var (
	// Taken from cpumanager.topoDualSocketHT
	topologyDualSocketHT = &topology.CPUTopology{
		NumCPUs:    12,
		NumSockets: 2,
		NumCores:   6,
		CPUDetails: map[int]topology.CPUInfo{
			0:  {CoreID: 0, SocketID: 0, NUMANodeID: 0},
			1:  {CoreID: 1, SocketID: 1, NUMANodeID: 1},
			2:  {CoreID: 2, SocketID: 0, NUMANodeID: 0},
			3:  {CoreID: 3, SocketID: 1, NUMANodeID: 1},
			4:  {CoreID: 4, SocketID: 0, NUMANodeID: 0},
			5:  {CoreID: 5, SocketID: 1, NUMANodeID: 1},
			6:  {CoreID: 0, SocketID: 0, NUMANodeID: 0},
			7:  {CoreID: 1, SocketID: 1, NUMANodeID: 1},
			8:  {CoreID: 2, SocketID: 0, NUMANodeID: 0},
			9:  {CoreID: 3, SocketID: 1, NUMANodeID: 1},
			10: {CoreID: 4, SocketID: 0, NUMANodeID: 0},
			11: {CoreID: 5, SocketID: 1, NUMANodeID: 1},
		},
	}
)

// Generate pod with given fields set, with pod.Policy=policyCFS
func testGeneratePodCPUSet(uid, cpuRequest, cpuLimit string) *v1.Pod {
	return testGeneratePod(policyCPUSet, uid, cpuRequest, cpuLimit)
}

// testCgroupCPUSet is used to generate cgroupCPUSet using in test
type testCgroupCPUSet struct {
	podSet sets.String
}

// Generate default cgroupCPUSet
func testGenerateCgroupCPUSet(tccc *testCgroupCPUSet) *cgroupCPUSet {
	// If pod state fileds (podSet, podToValue) are not set,
	// the default value of generated cgroupCPUSet is set to empty instead of Go's nil
	podSet := sets.NewString()
	if tccc.podSet != nil {
		podSet = tccc.podSet
	}

	return &cgroupCPUSet{
		podSet: podSet,
	}
}

// Check if the cgroup values in two cgroupCPUSet equal
func testEqualCgroupCPUSet(t *testing.T,
	expect *cgroupCPUSet, actual *cgroupCPUSet) {
	assert.Equal(t, expect.podSet, actual.podSet)
	assert.Equal(t, expect.cpusReserved, actual.cpusReserved)
	assert.Equal(t, expect.cpusShared, actual.cpusShared)
	assert.Equal(t, expect.podToCPUS, actual.podToCPUS)
}

func TestNewCgroupCPUSet(t *testing.T) {
	// When success, number of reserved CPUs is 2
	topologyFake := topologyDualSocketHT
	cpusAllFake := topologyFake.CPUDetails.CPUs()
	nodeAllocatableReservationSuccess := v1.ResourceList{
		v1.ResourceCPU: *resource.NewQuantity(2, resource.DecimalSI),
	}
	nodeAllocatableReservationFail := v1.ResourceList{
		v1.ResourceCPU: *resource.NewQuantity(0, resource.DecimalSI),
	}
	cpusReservedSuccess := cpuset.NewCPUSet(0, 6)
	cpusReservedFail := cpuset.NewCPUSet()
	cpusSpecificSuccess := cpuset.NewCPUSet(0, 1)
	cpusSpecificFail := cpuset.NewCPUSet()

	testCaseArray := []struct {
		description                string
		cpusSpecific               cpuset.CPUSet
		nodeAllocatableReservation v1.ResourceList
		cpusReserved               cpuset.CPUSet
		cpusReservedExp            cpuset.CPUSet
		expErrDiscover             error
		expErrTopology             error
		expErr                     error
	}{
		{
			description:    "Fail, error from topologyDiscoverFunc",
			expErrDiscover: fmt.Errorf("fake error"),
			expErr:         fmt.Errorf("fake error"),
		},
		{
			description:    "Fail, error from takeByTopologyFunc",
			expErrTopology: fmt.Errorf("fake error"),
			expErr:         fmt.Errorf("fake error"),
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
				if tc.expErrTopology == nil {
					return tc.cpusReserved, nil
				}
				return cpuset.NewCPUSet(), tc.expErrTopology
			}
			ccsExp := &cgroupCPUSet{
				podSet:             sets.NewString(),
				cpusReserved:       tc.cpusReservedExp,
				cpusShared:         cpusAllFake.Difference(tc.cpusReservedExp),
				topology:           topologyFake,
				podToCPUS:          make(map[string]cpuset.CPUSet),
				takeByTopologyFunc: takeByTopologyFunc,
			}

			ccs, err := NewCgroupCPUSet(topologyFake, takeByTopologyFunc,
				tc.cpusSpecific, tc.nodeAllocatableReservation)

			if tc.expErr == nil {
				assert.Nil(t, err)
				testEqualCgroupCPUSet(t, ccsExp, ccs.(*cgroupCPUSet))
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestCgroupCPUSetStart(t *testing.T) {
	topologyFake := topologyDualSocketHT
	cpusAllFake := topologyFake.CPUDetails.CPUs()
	cpusReserved := cpuset.NewCPUSet(0, 6)
	var takeByTopologyFunc cpumanager.TypeTakeByTopologyFunc
	ccs := &cgroupCPUSet{
		podSet:             sets.NewString(),
		cpusReserved:       cpusReserved,
		cpusShared:         cpusAllFake.Difference(cpusReserved),
		topology:           topologyFake,
		podToCPUS:          make(map[string]cpuset.CPUSet),
		takeByTopologyFunc: takeByTopologyFunc,
	}

	err := ccs.Start()

	assert.Nil(t, err, "Starting cgroupCPUSet failed")
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
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID: "1",
				},
			},
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
			}),
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: "Success, simple",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			pod:         testGeneratePodCPUSet("1", "", ""),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
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
			assert.Equal(t, tc.ccsAfter, ccs)
		})
	}
}

func TestCgroupCPUSetRemovePod(t *testing.T) {
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
			description: "Fail, pod not added yet",
			ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			pod:         testGeneratePodCPUSet("1", "", ""),
			ccsAfter:    testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description: "Success, simple",
			ccsBefore: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString("1"),
			}),
			pod:      testGeneratePodCPUSet("1", "", ""),
			ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			ccs := tc.ccsBefore

			err := ccs.RemovePod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, tc.ccsAfter, ccs)
		})
	}
}
