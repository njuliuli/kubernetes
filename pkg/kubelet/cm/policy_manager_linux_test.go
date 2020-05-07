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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager"
	cputopology "k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

var (
	// For cgroupCPUCFS
	cpuSmall      = "100m"
	cpuSmallShare = uint64(102)
	cpuSmallQuota = int64(10000)
	cpuLarge      = "200m"
	cpuLargeQuota = int64(20000)

	// Sample pods used in AddPod/RemovePod()
	podArray = []struct {
		description string
		pod         *v1.Pod
	}{
		{
			description: "PolicyDefault, in user space, using cpusShared pool",
			pod: testGeneratePodWithNamespace(
				policyDefault, "1", metav1.NamespaceDefault, "", ""),
		},
		{
			description: "PolicyDefault, in system namespace, using cpusReserved pool",
			pod: testGeneratePodWithNamespace(
				policyDefault, "2", metav1.NamespaceSystem, "", ""),
		},
		{
			description: "PolicyIsolated, cpusDedicated pool",
			pod: testGeneratePodWithNamespace(
				policyIsolated, "3", metav1.NamespaceDefault, "1", "1"),
		},
		{
			description: "PolicyCPUCFS, request = limit = empty",
			pod: testGeneratePodWithNamespace(
				policyCPUCFS, "4", metav1.NamespaceDefault, "", ""),
		},
		{
			description: "PolicyCPUCFS, request = limit",
			pod: testGeneratePodWithNamespace(
				policyCPUCFS, "5", metav1.NamespaceDefault, cpuSmall, cpuSmall),
		},
		{
			description: "PolicyCPUCFS, request < limit",
			pod: testGeneratePodWithNamespace(
				policyCPUCFS, "6", metav1.NamespaceDefault, cpuSmall, cpuLarge),
		},
	}
)

// Generate pod with given fields set
func testGeneratePodUIDAndPolicy(uid, policy string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: types.UID(uid),
		},
		Spec: v1.PodSpec{
			Policy: policy,
		},
	}
}

func testGeneratePod(policy, uid, cpuRequest, cpuLimit string) *v1.Pod {
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
			Policy: policy,
		},
	}
}

func testGeneratePodWithNamespace(policy, uid, namespace, cpuRequest, cpuLimit string) *v1.Pod {
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
			UID:       types.UID(uid),
			Namespace: namespace,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Resources: rr,
				},
			},
			Policy: policy,
		},
	}
}

// testCgroupCPUSet is used to generate policyManagerImpl using in test
type testPolicyManagerImpl struct {
	uidToPod      map[string]*v1.Pod
	cgroupCPUCFS  Cgroup
	cgroupCPUSet  Cgroup
	cgroupManager CgroupManager
}

// Generate default policyManagerImpl, with customized default values after initialization
func testGeneratePolicyManagerImpl(tpm *testPolicyManagerImpl) *policyManagerImpl {
	pm := &policyManagerImpl{
		uidToPod:      make(map[string]*v1.Pod),
		cgroupCPUCFS:  new(MockCgroup),
		cgroupCPUSet:  new(MockCgroup),
		cgroupManager: new(MockCgroupManager),
	}

	// If not set, some fields are set to customized default value
	tpmDefault := &testPolicyManagerImpl{}
	if !reflect.DeepEqual(tpm.uidToPod, tpmDefault.uidToPod) {
		pm.uidToPod = tpm.uidToPod
	}
	if !reflect.DeepEqual(tpm.cgroupCPUCFS, tpmDefault.cgroupCPUCFS) {
		pm.cgroupCPUCFS = tpm.cgroupCPUCFS
	}
	if !reflect.DeepEqual(tpm.cgroupCPUSet, tpmDefault.cgroupCPUSet) {
		pm.cgroupCPUSet = tpm.cgroupCPUSet
	}
	if !reflect.DeepEqual(tpm.cgroupManager, tpmDefault.cgroupManager) {
		pm.cgroupManager = tpm.cgroupManager
	}

	return pm
}

// testGenerateCgroupName is used to get CgroupName and CgroupPath for GetPodContainerName()
// Examples:
// cgroupName := CgroupName{"kubepods", "burstable", "pod1234-abcd-5678-efgh"}
// cgroupPath := "kubepods/burstable/pod1234-abcd-5678-efgh"
func testGenerateCgroupName(pod *v1.Pod) CgroupName {
	return CgroupName{"kubepods", "burstable", string(pod.UID)}
}

// Check if the cgroup values in two policyManagerImpl equal
func testEqualPolicyManagerImpl(t *testing.T,
	expect *policyManagerImpl, actual *policyManagerImpl) {
	assert.Equal(t, expect.uidToPod, actual.uidToPod)
}

func TestNewPolicyManagerImplAndStart(t *testing.T) {
	cpuTopologyFake := &cputopology.CPUTopology{}
	cpusSpecificFake := cpuset.NewCPUSet()
	nodeAllocatableReservationFake := v1.ResourceList{}

	testCaseArray := []struct {
		description        string
		errNewCgroupCPUCFS error
		errNewCgroupCPUSet error
		errCPUCFSStart     error
		errCPUSetStart     error
		expErr             error
		expPolicyManager   PolicyManager
	}{
		{
			description:      "Success, simple",
			expPolicyManager: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
		},
		// For NewPolicyManager(...)
		{
			description:        "Fail, error from NewCgroupCPUCFS",
			errNewCgroupCPUCFS: fmt.Errorf("fake error"),
			expErr:             fmt.Errorf("fake error"),
		},
		{
			description:        "Fail, error from NewCgroupCPUSet",
			errNewCgroupCPUSet: fmt.Errorf("fake error"),
			expErr:             fmt.Errorf("fake error"),
		},
		// For PolicyManager.Start()
		{
			description:    "Fail, error from errCPUCFS.Start()",
			errCPUCFSStart: fmt.Errorf("fake error"),
			expErr:         fmt.Errorf("fake error"),
		},
		{
			description:    "Fail, error from errCPUSet.Start()",
			errCPUSetStart: fmt.Errorf("fake error"),
			expErr:         fmt.Errorf("fake error"),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			// Setup
			cgroupCPUCFSMock := new(MockCgroup)
			cgroupCPUCFSMock.On("Start").Return(tc.errCPUCFSStart)
			newCgroupCPUCFS := func() (Cgroup, error) {
				if tc.errNewCgroupCPUCFS != nil {
					return nil, tc.errNewCgroupCPUCFS
				}
				return cgroupCPUCFSMock, nil
			}
			cgroupCPUSetMock := new(MockCgroup)
			cgroupCPUSetMock.On("Start").Return(tc.errCPUSetStart)
			newCgroupCPUSet := func(cpuTopology *cputopology.CPUTopology,
				takeByTopologyFunc cpumanager.TypeTakeByTopologyFunc,
				cpusSpecific cpuset.CPUSet,
				nodeAllocatableReservation v1.ResourceList) (Cgroup, error) {
				if tc.errNewCgroupCPUSet != nil {
					return nil, tc.errNewCgroupCPUSet
				}
				return cgroupCPUSetMock, nil
			}
			newPodContainerManager := func() PodContainerManager {
				return new(MockPodContainerManager)
			}
			activePodsFunc := func() []*v1.Pod {
				return []*v1.Pod{}
			}

			// Action:
			// NewPolicyManager(...) and then PolicyManager.Start(),
			// since they are only executed once and in this order
			newPolicyManager, err := NewPolicyManager(newCgroupCPUCFS, newCgroupCPUSet,
				new(MockCgroupManager), newPodContainerManager,
				cpuTopologyFake, cpusSpecificFake, nodeAllocatableReservationFake)
			if err == nil {
				err = newPolicyManager.Start(activePodsFunc)
			}

			// Assertion
			if tc.expErr == nil {
				assert.Nil(t, err)
				testEqualPolicyManagerImpl(t,
					tc.expPolicyManager.(*policyManagerImpl),
					newPolicyManager.(*policyManagerImpl))
				cgroupCPUCFSMock.AssertExpectations(t)
				cgroupCPUSetMock.AssertExpectations(t)
			} else {
				assert.Error(t, err)
			}
		})
	}

}

// Test for .AddPod(...) is like an integration test,
// and details of its logic is broken down into test for its internal dependencies
func TestPolicyManagerImplAddPod(t *testing.T) {
	// The construction of test table is completed by categories below
	type testCaseStruct struct {
		description string
		pmBefore    *policyManagerImpl
		pod         *v1.Pod
		mockMap     map[*v1.Pod]*ResourceConfig
		pmAfter     *policyManagerImpl
		expErr      error
	}

	testCaseArray := []testCaseStruct{}
	// Total pods: 0 -> 1
	endWithOnePod := true
	// Total pods: N -> N+1
	endWithMultiplePods := true
	// Total pods: x -> N+1 (recover from bogus states)
	startWithBogusState := true

	// Total pod: 0 -> 1
	// Each of the pod will be added as the first pod
	if endWithOnePod {
		// Host-global Cgroups is initialized with default one
		ccsFake := testGenerateCgroupCPUSet(&testCgroupCPUSet{})
		cpusReservedFake := ccsFake.cpusReserved.String()
		cpusSharedFake := ccsFake.cpusShared.String()

		// When PolicyManager has 0 pod, add 1 pod into it
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[0].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
			pod: podArray[0].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
					),
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
					),
				}),
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[1].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
			pod: podArray[1].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[1].pod.UID),
					),
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[1].pod.UID),
					),
				}),
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[2].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
			pod: podArray[2].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpuset.NewCPUSet(1).String()),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[2].pod.UID): podArray[2].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[2].pod.UID),
					),
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[2].pod.UID),
					),
					cpusShared: testCPUSShared.
						Difference(cpuset.NewCPUSet(1)),
					podToCPUS: map[string]cpuset.CPUSet{
						string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
					},
				}),
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[3].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
			pod: podArray[3].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[3].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSharesMin),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[3].pod.UID): podArray[3].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[3].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[3].pod.UID): cpuSharesMin,
					},
					podToCPUQuota:  map[string]int64{},
					podToCPUPeriod: map[string]uint64{},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[3].pod.UID),
					),
				}),
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[4].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
			pod: podArray[4].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[4].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuSmallQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[4].pod.UID): podArray[4].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[4].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[4].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[4].pod.UID): cpuSmallQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[4].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[4].pod.UID),
					),
				}),
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[5].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
			pod: podArray[5].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[5].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuLargeQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[5].pod.UID): podArray[5].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[5].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[5].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[5].pod.UID): cpuLargeQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[5].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[5].pod.UID),
					),
				}),
			}),
		})
	}

	// Total pod: N -> N+1
	// When pods are added accumulated, from the same podArray above
	if endWithMultiplePods {
		// At the beginning, 0 pod
		pm := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})

		// Host-global Cgroups is initialized with default one
		ccsFake := testGenerateCgroupCPUSet(&testCgroupCPUSet{
			podSet: sets.NewString(
				string(podArray[0].pod.UID),
			),
		})
		cpusReservedFake := ccsFake.cpusReserved.String()
		cpusSharedFake := ccsFake.cpusShared.String()

		// Adding sampleArray[0].pod,
		// (policyDefault, "1", metav1.NamespaceDefault, "", "")
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[0].pod (%q)",
				podArray[0].description),
			pmBefore: pm,
			pod:      podArray[0].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
					),
				}),
				cgroupCPUSet: ccsFake,
			}),
		})

		// Accumulately adding sampleArray[1].pod
		// (policyDefault, "2", metav1.NamespaceSystem, "", "")
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[1].pod (%q)",
				podArray[1].description),
			pmBefore: pm,
			pod:      podArray[1].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
					),
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
					),
				}),
			}),
		})

		// Update host-global Cgroup for the next pod
		ccsFake = testGenerateCgroupCPUSet(&testCgroupCPUSet{
			podSet: sets.NewString(
				string(podArray[0].pod.UID),
				string(podArray[1].pod.UID),
				string(podArray[2].pod.UID),
			),
			cpusShared: testCPUSShared.
				Difference(cpuset.NewCPUSet(1)),
			podToCPUS: map[string]cpuset.CPUSet{
				string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
			},
		})
		cpusDedicatedFake := ccsFake.podToCPUS[string(podArray[2].pod.UID)].String()
		cpusReservedFake = ccsFake.cpusReserved.String()
		cpusSharedFake = ccsFake.cpusShared.String()

		// Accumulately adding sampleArray[2].pod
		// (policyIsolated, "3", metav1.NamespaceDefault, "1", "1")
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[2].pod (%q)",
				podArray[2].description),
			pmBefore: pm,
			pod:      podArray[2].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusDedicatedFake),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
					),
				}),
				cgroupCPUSet: ccsFake,
			}),
		})

		// Accumulately adding sampleArray[3].pod
		// (policyCPUCFS, "4", metav1.NamespaceDefault, "", "")
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[3].pod (%q)",
				podArray[3].description),
			pmBefore: pm,
			pod:      podArray[3].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusDedicatedFake),
				},
				podArray[3].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSharesMin),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
					string(podArray[3].pod.UID): podArray[3].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[3].pod.UID): cpuSharesMin,
					},
					podToCPUQuota:  map[string]int64{},
					podToCPUPeriod: map[string]uint64{},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
					),
					cpusShared: testCPUSShared.
						Difference(cpuset.NewCPUSet(1)),
					podToCPUS: map[string]cpuset.CPUSet{
						string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
					},
				}),
			}),
		})

		// Accumulately adding sampleArray[4].pod
		// (policyCPUCFS, "5", metav1.NamespaceDefault, cpuSmall, cpuSmall)
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[4].pod (%q)",
				podArray[4].description),
			pmBefore: pm,
			pod:      podArray[4].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusDedicatedFake),
				},
				podArray[3].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSharesMin),
				},
				podArray[4].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuSmallQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
					string(podArray[3].pod.UID): podArray[3].pod,
					string(podArray[4].pod.UID): podArray[4].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
						string(podArray[4].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[3].pod.UID): cpuSharesMin,
						string(podArray[4].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[4].pod.UID): cpuSmallQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[4].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
						string(podArray[4].pod.UID),
					),
					cpusShared: testCPUSShared.
						Difference(cpuset.NewCPUSet(1)),
					podToCPUS: map[string]cpuset.CPUSet{
						string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
					},
				}),
			}),
		})

		// Accumulately adding sampleArray[5].pod
		// (policyCPUCFS, "6", metav1.NamespaceDefault, cpuSmall, cpuLarge)
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[5].pod (%q)",
				podArray[5].description),
			pmBefore: pm,
			pod:      podArray[5].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusDedicatedFake),
				},
				podArray[3].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSharesMin),
				},
				podArray[4].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuSmallQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
				podArray[5].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuLargeQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
					string(podArray[3].pod.UID): podArray[3].pod,
					string(podArray[4].pod.UID): podArray[4].pod,
					string(podArray[5].pod.UID): podArray[5].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
						string(podArray[4].pod.UID),
						string(podArray[5].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[3].pod.UID): cpuSharesMin,
						string(podArray[4].pod.UID): cpuSmallShare,
						string(podArray[5].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[4].pod.UID): cpuSmallQuota,
						string(podArray[5].pod.UID): cpuLargeQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[4].pod.UID): cpuPeriodDefault,
						string(podArray[5].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
						string(podArray[4].pod.UID),
						string(podArray[5].pod.UID),
					),
					cpusShared: testCPUSShared.
						Difference(cpuset.NewCPUSet(1)),
					podToCPUS: map[string]cpuset.CPUSet{
						string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
					},
				}),
			}),
		})
	}
	// Total pods: x -> N+1 (recover from bogus states)
	if startWithBogusState {
		// Update host-global Cgroup for adding podArray[2].pod
		// (policyIsolated, "3", metav1.NamespaceDefault, "1", "1")
		ccsFake := testGenerateCgroupCPUSet(&testCgroupCPUSet{
			podSet: sets.NewString(
				string(podArray[0].pod.UID),
				string(podArray[1].pod.UID),
				string(podArray[2].pod.UID),
				string(podArray[4].pod.UID),
			),
			cpusShared: testCPUSShared.
				Difference(cpuset.NewCPUSet(1)),
			podToCPUS: map[string]cpuset.CPUSet{
				string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
			},
		})
		cpusDedicatedFake := ccsFake.podToCPUS[string(podArray[2].pod.UID)].String()
		cpusReservedFake := ccsFake.cpusReserved.String()
		cpusSharedFake := ccsFake.cpusShared.String()

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, recover lost podArray[1], podArray[2], podArray[4] before adding this podArray[0]"),
			// 0 pods at the beginning, after kubelet restart,
			// the global state of CPUSet is lost for adding podArray[2].pod
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
			pod: podArray[0].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusDedicatedFake),
				},
				podArray[4].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuSmallQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
			},
			// 4 pods afterwards, 3 pods recovered and 1 pods added
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
					string(podArray[4].pod.UID): podArray[4].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[4].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[4].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[4].pod.UID): cpuSmallQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[4].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: ccsFake,
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing podArray[3] and adding podArray[1] before adding this podArray[0]"),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[2].pod.UID): podArray[2].pod,
					string(podArray[3].pod.UID): podArray[3].pod,
					string(podArray[4].pod.UID): podArray[4].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
						string(podArray[4].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[3].pod.UID): cpuSharesMin,
						string(podArray[4].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[4].pod.UID): cpuSmallQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[4].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
						string(podArray[4].pod.UID),
					),
					cpusShared: testCPUSShared.
						Difference(cpuset.NewCPUSet(1)),
					podToCPUS: map[string]cpuset.CPUSet{
						string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
					},
				}),
			}),
			pod: podArray[0].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusDedicatedFake),
				},
				podArray[4].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuSmallQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
					string(podArray[4].pod.UID): podArray[4].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[4].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[4].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[4].pod.UID): cpuSmallQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[4].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: ccsFake,
			}),
		})
	}

	// testCaseArray is built by categories above
	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			pm := tc.pmBefore
			pcmMock := new(MockPodContainerManager)
			pm.newPodContainerManager = func() PodContainerManager {
				return pcmMock
			}
			cmMock := pm.cgroupManager.(*MockCgroupManager)
			activePods := []*v1.Pod{}
			for pod, resourceConfig := range tc.mockMap {
				activePods = append(activePods, pod)
				cgroupName := testGenerateCgroupName(pod)
				pcmMock.On("GetPodContainerName", pod).
					Return(cgroupName, "").
					Once()
				cgroupConfig := &CgroupConfig{
					Name:               cgroupName,
					ResourceParameters: resourceConfig,
				}
				cmMock.On("Update", cgroupConfig).
					Return(nil).
					Once()
			}
			pm.activePodsFunc = func() []*v1.Pod {
				return activePods
			}

			err := pm.AddPod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			testEqualPolicyManagerImpl(t, tc.pmAfter, pm)
			pcmMock.AssertExpectations(t)
			cmMock.AssertExpectations(t)
		})
	}
}

func TestPolicyManagerImplGetPodsForAdd(t *testing.T) {
	type testCaseStruct struct {
		description string
		pm          *policyManagerImpl
		activePods  []*v1.Pod
		pod         *v1.Pod
		uidToRemove sets.String
		podToAdd    map[string]*v1.Pod
	}
	testCaseArray := []testCaseStruct{}

	// Simple cases
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: fmt.Sprintf("Simple, 0 existing pods"),
			pm:          testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			activePods: []*v1.Pod{
				podArray[0].pod,
			},
			pod:         podArray[0].pod,
			uidToRemove: sets.NewString(),
			podToAdd: map[string]*v1.Pod{
				string(podArray[0].pod.UID): podArray[0].pod,
			},
		},
		{
			description: fmt.Sprintf("Simple, 2 existing pods"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[0].pod,
				podArray[1].pod,
				podArray[2].pod,
			},
			pod:         podArray[0].pod,
			uidToRemove: sets.NewString(),
			podToAdd: map[string]*v1.Pod{
				string(podArray[0].pod.UID): podArray[0].pod,
			},
		},
	}...)

	// Complicated cases, with PolicyManager and activePods not match
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: fmt.Sprintf("Simple, 1 existing pods"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[0].pod,
				podArray[1].pod,
			},
			pod:         podArray[0].pod,
			uidToRemove: sets.NewString(),
			podToAdd: map[string]*v1.Pod{
				string(podArray[0].pod.UID): podArray[0].pod,
			},
		},
		{
			description: fmt.Sprintf("Bogus PolicyManager, 1 less existing pods"),
			pm:          testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			activePods: []*v1.Pod{
				podArray[0].pod,
				podArray[1].pod,
			},
			pod:         podArray[0].pod,
			uidToRemove: sets.NewString(),
			podToAdd: map[string]*v1.Pod{
				string(podArray[0].pod.UID): podArray[0].pod,
				string(podArray[1].pod.UID): podArray[1].pod,
			},
		},
		{
			description: fmt.Sprintf("Bogus PolicyManager, 1 more existing pods"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[0].pod,
				podArray[1].pod,
			},
			pod: podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[2].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{
				string(podArray[0].pod.UID): podArray[0].pod,
			},
		},
		{
			description: fmt.Sprintf("Bogus PolicyManager, 1 more and 1 less existing pods"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[2].pod.UID): podArray[2].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[0].pod,
				podArray[1].pod,
			},
			pod: podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[2].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{
				string(podArray[0].pod.UID): podArray[0].pod,
				string(podArray[1].pod.UID): podArray[1].pod,
			},
		},
		{
			description: fmt.Sprintf("Bogus PolicyManager, this pod is not active"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[1].pod,
			},
			pod:         podArray[0].pod,
			uidToRemove: sets.NewString(),
			podToAdd:    map[string]*v1.Pod{},
		},
		{
			description: fmt.Sprintf("Bogus activePods, new pod already exist"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[0].pod,
				podArray[1].pod,
			},
			pod:         podArray[0].pod,
			uidToRemove: sets.NewString(),
			podToAdd:    map[string]*v1.Pod{},
		},
		{
			description: fmt.Sprintf("Bogus PolicyManager and activePods"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[0].pod,
				podArray[1].pod,
			},
			pod: podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[2].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{
				string(podArray[1].pod.UID): podArray[1].pod,
			},
		},
	}...)

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			pm := tc.pm
			pm.activePodsFunc = func() []*v1.Pod {
				return tc.activePods
			}

			uidToRemove, podToAdd := pm.getPodsForAdd(tc.pod)

			assert.Equal(t, tc.uidToRemove, uidToRemove)
			assert.Equal(t, tc.podToAdd, podToAdd)
		})
	}
}

func TestPolicyManagerImplRemovePod(t *testing.T) {
	type testCaseStruct struct {
		description string
		pmBefore    *policyManagerImpl
		pod         *v1.Pod
		mockMap     map[*v1.Pod]*ResourceConfig
		pmAfter     *policyManagerImpl
		expErr      error
	}

	testCaseArray := []testCaseStruct{}
	// Total pod: 1 -> 0
	endWithOnePod := true
	// Total pod: N+1 -> N
	endWithMultiplePods := true
	// Total pods: x -> N-1 (recover from bogus states)
	startWithBogusState := true

	// Total pod: 1 -> 0
	// Each of the pod will be removed as the only existing pod
	// The data is the exact the reverse of TestPolicyManagerAddPod() above
	if endWithOnePod {
		// When the only existing pod is removed
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[0].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
					),
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
					),
				}),
			}),
			pod:     podArray[0].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[1].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[1].pod.UID),
					),
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[1].pod.UID),
					),
				}),
			}),
			pod:     podArray[1].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[2].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[2].pod.UID): podArray[2].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[2].pod.UID),
					),
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[2].pod.UID),
					),
					cpusShared: testCPUSShared.
						Difference(cpuset.NewCPUSet(1)),
					podToCPUS: map[string]cpuset.CPUSet{
						string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
					},
				}),
			}),
			pod:     podArray[2].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[3].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[3].pod.UID): podArray[3].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[3].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[3].pod.UID): cpuSharesMin,
					},
					podToCPUQuota:  map[string]int64{},
					podToCPUPeriod: map[string]uint64{},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[3].pod.UID),
					),
				}),
			}),
			pod:     podArray[3].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[4].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[4].pod.UID): podArray[4].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[4].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[4].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[4].pod.UID): cpuSmallQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[4].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[4].pod.UID),
					),
				}),
			}),
			pod:     podArray[4].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[5].description),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[5].pod.UID): podArray[5].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[5].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[5].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[5].pod.UID): cpuLargeQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[5].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[5].pod.UID),
					),
				}),
			}),
			pod:     podArray[5].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
		})
	}

	// Total pod: N+1 -> N
	// When pods are removed accumulated, from the same podArray above
	// This process is exact the reverse order of TestPolicyManagerAddPod() above
	if endWithMultiplePods {
		// At the beginning, 5 pods
		pm := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod: map[string]*v1.Pod{
				string(podArray[0].pod.UID): podArray[0].pod,
				string(podArray[1].pod.UID): podArray[1].pod,
				string(podArray[2].pod.UID): podArray[2].pod,
				string(podArray[3].pod.UID): podArray[3].pod,
				string(podArray[4].pod.UID): podArray[4].pod,
				string(podArray[5].pod.UID): podArray[5].pod,
			},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
				podSet: sets.NewString(
					string(podArray[0].pod.UID),
					string(podArray[1].pod.UID),
					string(podArray[2].pod.UID),
					string(podArray[3].pod.UID),
					string(podArray[4].pod.UID),
					string(podArray[5].pod.UID),
				),
				podToCPUShares: map[string]uint64{
					string(podArray[3].pod.UID): cpuSharesMin,
					string(podArray[4].pod.UID): cpuSmallShare,
					string(podArray[5].pod.UID): cpuSmallShare,
				},
				podToCPUQuota: map[string]int64{
					string(podArray[4].pod.UID): cpuSmallQuota,
					string(podArray[5].pod.UID): cpuLargeQuota,
				},
				podToCPUPeriod: map[string]uint64{
					string(podArray[4].pod.UID): cpuPeriodDefault,
					string(podArray[5].pod.UID): cpuPeriodDefault,
				},
			}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
				podSet: sets.NewString(
					string(podArray[0].pod.UID),
					string(podArray[1].pod.UID),
					string(podArray[2].pod.UID),
					string(podArray[3].pod.UID),
					string(podArray[4].pod.UID),
					string(podArray[5].pod.UID),
				),
				cpusShared: testCPUSShared.
					Difference(cpuset.NewCPUSet(1)),
				podToCPUS: map[string]cpuset.CPUSet{
					string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
				},
			}),
		})

		// Update host-global Cgroup
		ccsFake := testGenerateCgroupCPUSet(&testCgroupCPUSet{
			podSet: sets.NewString(
				string(podArray[0].pod.UID),
				string(podArray[1].pod.UID),
				string(podArray[2].pod.UID),
				string(podArray[3].pod.UID),
				string(podArray[4].pod.UID),
			),
			cpusShared: testCPUSShared.
				Difference(cpuset.NewCPUSet(1)),
			podToCPUS: map[string]cpuset.CPUSet{
				string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
			},
		})
		cpusDedicatedFake := ccsFake.podToCPUS[string(podArray[2].pod.UID)].String()
		cpusReservedFake := ccsFake.cpusReserved.String()
		cpusSharedFake := ccsFake.cpusShared.String()

		// Accumulately removing sampleArray[5].pod
		// (policyCPUCFS, "6", metav1.NamespaceDefault, cpuSmall, cpuLarge)
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[5].pod (%q)",
				podArray[5].description),
			pmBefore: pm,
			pod:      podArray[5].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusDedicatedFake),
				},
				podArray[3].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSharesMin),
				},
				podArray[4].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuSmallQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
					string(podArray[3].pod.UID): podArray[3].pod,
					string(podArray[4].pod.UID): podArray[4].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
						string(podArray[4].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[3].pod.UID): cpuSharesMin,
						string(podArray[4].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[4].pod.UID): cpuSmallQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[4].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: ccsFake,
			}),
		})

		// Accumulately removing sampleArray[4].pod
		// (policyCPUCFS, "5", metav1.NamespaceDefault, cpuSmall, cpuSmall)
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[4].pod (%q)",
				podArray[4].description),
			pmBefore: pm,
			pod:      podArray[4].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusDedicatedFake),
				},
				podArray[3].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSharesMin),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
					string(podArray[3].pod.UID): podArray[3].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[3].pod.UID): cpuSharesMin,
					},
					podToCPUQuota:  map[string]int64{},
					podToCPUPeriod: map[string]uint64{},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
					),
					cpusShared: testCPUSShared.
						Difference(cpuset.NewCPUSet(1)),
					podToCPUS: map[string]cpuset.CPUSet{
						string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
					},
				}),
			}),
		})

		// Accumulately removing sampleArray[3].pod
		// (policyCPUCFS, "4", metav1.NamespaceDefault, "", "")
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[3].pod (%q)",
				podArray[3].description),
			pmBefore: pm,
			pod:      podArray[3].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusDedicatedFake),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
					),
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
					),
					cpusShared: testCPUSShared.
						Difference(cpuset.NewCPUSet(1)),
					podToCPUS: map[string]cpuset.CPUSet{
						string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
					},
				}),
			}),
		})

		// Update host-global Cgroup
		ccsFake = testGenerateCgroupCPUSet(&testCgroupCPUSet{
			podSet: sets.NewString(
				string(podArray[0].pod.UID),
				string(podArray[1].pod.UID),
			),
		})
		cpusReservedFake = ccsFake.cpusReserved.String()
		cpusSharedFake = ccsFake.cpusShared.String()

		// Accumulately removing sampleArray[2].pod
		// (policyIsolated, "3", metav1.NamespaceDefault, "1", "1")
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[2].pod (%q)",
				podArray[2].description),
			pmBefore: pm,
			pod:      podArray[2].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
					),
				}),
				cgroupCPUSet: ccsFake,
			}),
		})

		// Accumulately removing sampleArray[1].pod
		// (policyDefault, "2", metav1.NamespaceSystem, "", "")
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[1].pod (%q)",
				podArray[1].description),
			pmBefore: pm,
			pod:      podArray[1].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
					),
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
					),
				}),
			}),
		})

		// Removing sampleArray[0].pod,
		// (policyDefault, "1", metav1.NamespaceDefault, "", "")
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[0].pod (%q)",
				podArray[0].description),
			pmBefore: pm,
			pod:      podArray[0].pod,
			mockMap:  map[*v1.Pod]*ResourceConfig{},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
		})
	}

	// Total pods: x -> N-1 (recover from bogus states)
	if startWithBogusState {
		// Update host-global Cgroup for adding podArray[2].pod
		// (policyIsolated, "3", metav1.NamespaceDefault, "1", "1")
		ccsFake := testGenerateCgroupCPUSet(&testCgroupCPUSet{
			podSet: sets.NewString(
				string(podArray[0].pod.UID),
				string(podArray[1].pod.UID),
				string(podArray[2].pod.UID),
				string(podArray[4].pod.UID),
			),
			cpusShared: testCPUSShared.
				Difference(cpuset.NewCPUSet(1)),
			podToCPUS: map[string]cpuset.CPUSet{
				string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
			},
		})
		cpusDedicatedFake := ccsFake.podToCPUS[string(podArray[2].pod.UID)].String()
		cpusReservedFake := ccsFake.cpusReserved.String()
		cpusSharedFake := ccsFake.cpusShared.String()

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, recover lost podArray[0], podArray[1], podArray[2], podArray[4], then skip removing this podArray[3]"),
			// 0 pods at the beginning, after kubelet restart,
			// the global state of CPUSet is lost for adding podArray[2].pod
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod:     map[string]*v1.Pod{},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
			}),
			pod: podArray[3].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusDedicatedFake),
				},
				podArray[4].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuSmallQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
			},
			// 4 pods afterwards, 4 pods recovered and this pod is skipped
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
					string(podArray[4].pod.UID): podArray[4].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[4].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[4].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[4].pod.UID): cpuSmallQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[4].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: ccsFake,
			}),
		})

		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing podArray[5] and adding podArray[0] before removing this podArray[3]"),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
					string(podArray[3].pod.UID): podArray[3].pod,
					string(podArray[4].pod.UID): podArray[4].pod,
					string(podArray[5].pod.UID): podArray[5].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
						string(podArray[4].pod.UID),
						string(podArray[5].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[3].pod.UID): cpuSharesMin,
						string(podArray[4].pod.UID): cpuSmallShare,
						string(podArray[5].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[4].pod.UID): cpuSmallQuota,
						string(podArray[5].pod.UID): cpuLargeQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[4].pod.UID): cpuPeriodDefault,
						string(podArray[5].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{
					podSet: sets.NewString(
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[3].pod.UID),
						string(podArray[4].pod.UID),
						string(podArray[5].pod.UID),
					),
					cpusShared: testCPUSShared.
						Difference(cpuset.NewCPUSet(1)),
					podToCPUS: map[string]cpuset.CPUSet{
						string(podArray[2].pod.UID): cpuset.NewCPUSet(1),
					},
				}),
			}),
			pod: podArray[3].pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusDedicatedFake),
				},
				podArray[4].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuSmallQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
					string(podArray[4].pod.UID): podArray[4].pod,
				},
				cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{
					podSet: sets.NewString(
						string(podArray[0].pod.UID),
						string(podArray[1].pod.UID),
						string(podArray[2].pod.UID),
						string(podArray[4].pod.UID),
					),
					podToCPUShares: map[string]uint64{
						string(podArray[4].pod.UID): cpuSmallShare,
					},
					podToCPUQuota: map[string]int64{
						string(podArray[4].pod.UID): cpuSmallQuota,
					},
					podToCPUPeriod: map[string]uint64{
						string(podArray[4].pod.UID): cpuPeriodDefault,
					},
				}),
				cgroupCPUSet: ccsFake,
			}),
		})
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			pm := tc.pmBefore
			pcmMock := new(MockPodContainerManager)
			pm.newPodContainerManager = func() PodContainerManager {
				return pcmMock
			}
			cmMock := pm.cgroupManager.(*MockCgroupManager)
			activePods := []*v1.Pod{}
			for pod, resourceConfig := range tc.mockMap {
				activePods = append(activePods, pod)
				cgroupName := testGenerateCgroupName(pod)
				pcmMock.On("GetPodContainerName", pod).
					Return(cgroupName, "").
					Once()
				cgroupConfig := &CgroupConfig{
					Name:               cgroupName,
					ResourceParameters: resourceConfig,
				}
				cmMock.On("Update", cgroupConfig).
					Return(nil).
					Once()
			}
			pm.activePodsFunc = func() []*v1.Pod {
				return activePods
			}

			err := pm.RemovePod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			testEqualPolicyManagerImpl(t, tc.pmAfter, pm)
			pcmMock.AssertExpectations(t)
			cmMock.AssertExpectations(t)
		})
	}
}

func TestPolicyManagerImplGetPodsForRemove(t *testing.T) {
	type testCaseStruct struct {
		description string
		pm          *policyManagerImpl
		activePods  []*v1.Pod
		pod         *v1.Pod
		uidToRemove sets.String
		podToAdd    map[string]*v1.Pod
	}
	testCaseArray := []testCaseStruct{}

	// Simple cases, this pod is in PolicyManager, but not in activePods
	// This is the case when the pod completes successfully
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: fmt.Sprintf("Simple, 0 pods afterwards, this pod is not active"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			activePods: []*v1.Pod{},
			pod:        podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{},
		},
		{
			description: fmt.Sprintf("Simple, 2 pods afterwards, this pod is not active"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[1].pod,
				podArray[2].pod,
			},
			pod: podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{},
		},
	}...)

	// Simple cases, this pod is in both PolicyManager and activePods
	// This is the case when the pod is delelted by the user
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: fmt.Sprintf("Simple, 0 pods afterwards, this pod is active"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[0].pod,
			},
			pod: podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{},
		},
		{
			description: fmt.Sprintf("Simple, 2 pods afterwards, this pod is active"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[0].pod,
				podArray[1].pod,
				podArray[2].pod,
			},
			pod: podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{},
		},
	}...)

	// Complicated cases, with PolicyManager and activePods not match
	// Here we only list the case that this pod completes successfully,
	// e.g. this pod is in PolicyManager but not in activePods
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: fmt.Sprintf("Simple, 1 existing pods"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[1].pod,
			},
			pod: podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{},
		},
		{
			description: fmt.Sprintf("Bogus PolicyManager, 1 less existing pods"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[1].pod,
			},
			pod: podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{
				string(podArray[1].pod.UID): podArray[1].pod,
			},
		},
		{
			description: fmt.Sprintf("Bogus PolicyManager, 1 more existing pods"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[1].pod,
			},
			pod: podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
				string(podArray[2].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{},
		},
		{
			description: fmt.Sprintf("Bogus PolicyManager, 1 more and 1 less existing pods"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[2].pod.UID): podArray[2].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[1].pod,
			},
			pod: podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
				string(podArray[2].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{
				string(podArray[1].pod.UID): podArray[1].pod,
			},
		},
		{
			description: fmt.Sprintf("Bogus activePods, this pod already removed"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[1].pod,
			},
			pod:         podArray[0].pod,
			uidToRemove: sets.NewString(),
			podToAdd:    map[string]*v1.Pod{},
		},
		{
			description: fmt.Sprintf("Bogus PolicyManager and activePods"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[2].pod.UID): podArray[2].pod,
				},
			}),
			activePods: []*v1.Pod{
				podArray[1].pod,
			},
			pod: podArray[0].pod,
			uidToRemove: sets.NewString(
				string(podArray[2].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{
				string(podArray[1].pod.UID): podArray[1].pod,
			},
		},
	}...)

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			pm := tc.pm
			pm.activePodsFunc = func() []*v1.Pod {
				return tc.activePods
			}

			uidToRemove, podToAdd := pm.getPodsForRemove(tc.pod)

			assert.Equal(t, tc.uidToRemove, uidToRemove)
			assert.Equal(t, tc.podToAdd, podToAdd)
		})
	}
}

func TestPolicyManagerImplUpdate(t *testing.T) {
	type testCaseStruct struct {
		description  string
		pmBefore     *policyManagerImpl
		uidToRemove  sets.String
		errRemovePod error
		podToAdd     map[string]*v1.Pod
		errAddPod    error
		pmAfter      *policyManagerImpl
		expErr       error
	}
	testCaseArray := []testCaseStruct{}

	// Success, for all supported policies
	for _, p := range podArray {
		testCaseArray = append(testCaseArray, []testCaseStruct{
			{
				description: fmt.Sprintf("Success, remove pod, %q", p.description),
				pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
					uidToPod: map[string]*v1.Pod{
						string(p.pod.UID): p.pod,
					},
				}),
				uidToRemove: sets.NewString(
					string(p.pod.UID),
				),
				pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
					uidToPod: map[string]*v1.Pod{},
				}),
			},
			{
				description: fmt.Sprintf("Success, add pod, %q", p.description),
				pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
					uidToPod: map[string]*v1.Pod{},
				}),
				podToAdd: map[string]*v1.Pod{
					string(p.pod.UID): p.pod,
				},
				pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
					uidToPod: map[string]*v1.Pod{
						string(p.pod.UID): p.pod,
					},
				}),
			},
		}...)
	}

	// Success, for unknown policies
	podPolicyUnknown := testGeneratePodUIDAndPolicy("1", policyUnknown)
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: fmt.Sprintf("Success, remove pod, policyUnknown"),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podPolicyUnknown.UID): podPolicyUnknown,
				},
			}),
			uidToRemove: sets.NewString(
				string(podPolicyUnknown.UID),
			),
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{},
			}),
		},
		{
			description: fmt.Sprintf("Success, add pod, policyUnknown"),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{},
			}),
			podToAdd: map[string]*v1.Pod{
				string(podPolicyUnknown.UID): podPolicyUnknown,
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podPolicyUnknown.UID): podPolicyUnknown,
				},
			}),
		},
	}...)

	// Success cases, without errors from Cgroup.AddPod/RemovePod
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: fmt.Sprintf("Success, with only uidToRemove"),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
			),
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{},
			}),
		},
		{
			description: fmt.Sprintf("Success, with only podToAdd"),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{},
			}),
			podToAdd: map[string]*v1.Pod{
				string(podArray[0].pod.UID): podArray[0].pod,
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
		},
		{
			description: fmt.Sprintf("Success, with both uidToRemove and podToAdd"),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{
				string(podArray[1].pod.UID): podArray[1].pod,
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
				},
			}),
		},
	}...)

	// Fail cases, with errors from Cgroup.AddPod/RemovePod
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: fmt.Sprintf("Fail, with error from Cgroup.AddPod"),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
			),
			podToAdd: map[string]*v1.Pod{
				string(podArray[1].pod.UID): podArray[1].pod,
			},
			errAddPod: fmt.Errorf("fake error"),
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
				},
			}),
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: fmt.Sprintf("Fail, with error from Cgroup.RemovePod"),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
			),
			errRemovePod: fmt.Errorf("fake error"),
			podToAdd: map[string]*v1.Pod{
				string(podArray[1].pod.UID): podArray[1].pod,
			},
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
				},
			}),
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: fmt.Sprintf("Fail, with error from both Cgroup.AddPod and Cgroup.RemovePod"),
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			uidToRemove: sets.NewString(
				string(podArray[0].pod.UID),
			),
			errRemovePod: fmt.Errorf("fake error"),
			podToAdd: map[string]*v1.Pod{
				string(podArray[1].pod.UID): podArray[1].pod,
			},
			errAddPod: fmt.Errorf("fake error"),
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[1].pod.UID): podArray[1].pod,
				},
			}),
			expErr: fmt.Errorf("fake error"),
		},
	}...)

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			pm := tc.pmBefore
			ccsMock := pm.cgroupCPUSet.(*MockCgroup)
			cccMock := pm.cgroupCPUCFS.(*MockCgroup)
			// For simplicity, return error/nil on all call to Cgroup.RemovePod
			for podUID := range tc.uidToRemove {
				ccsMock.On("RemovePod", podUID).
					Return(tc.errRemovePod).Once()
				cccMock.On("RemovePod", podUID).
					Return(tc.errRemovePod).Once()
			}
			for _, pod := range tc.podToAdd {
				ccsMock.On("AddPod", pod).
					Return(tc.errAddPod).Once()
				cccMock.On("AddPod", pod).
					Return(tc.errAddPod).Once()
			}

			err := pm.update(tc.uidToRemove, tc.podToAdd)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			testEqualPolicyManagerImpl(t, tc.pmAfter, pm)
			ccsMock.AssertExpectations(t)
			cccMock.AssertExpectations(t)
		})
	}
}

func TestPolicyManagerImplUpdateToHost(t *testing.T) {
	type mockStruct struct {
		rcCCS        *ResourceConfig
		isTrackedCCS bool
		rcCCC        *ResourceConfig
		isTrackedCCC bool
		rc           *ResourceConfig
		errCM        error
	}
	type testCaseStruct struct {
		description string
		pm          *policyManagerImpl
		mockMap     map[*v1.Pod]*mockStruct
		expErr      error
	}
	testCaseArray := []testCaseStruct{}

	// Success cases
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: fmt.Sprintf("Success, 0 pods in PolicyManager"),
			pm:          testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			mockMap:     map[*v1.Pod]*mockStruct{},
		},
		{
			description: fmt.Sprintf("Success, 1 pods in PolicyManager, simple returns"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			// Mock return has no relation with true response of Cgroups on podArray[0].pod
			mockMap: map[*v1.Pod]*mockStruct{
				podArray[0].pod: {
					rcCCS:        &ResourceConfig{},
					isTrackedCCS: true,
					rcCCC:        &ResourceConfig{},
					isTrackedCCC: true,
					rc:           &ResourceConfig{},
				},
			},
		},
		{
			description: fmt.Sprintf("Success, 1 pods in PolicyManager, complex returns"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			mockMap: map[*v1.Pod]*mockStruct{
				podArray[0].pod: {
					rcCCS: &ResourceConfig{
						CpusetCpus: testCopyString(cpuset.NewCPUSet(1).String()),
					},
					isTrackedCCS: true,
					rcCCC: &ResourceConfig{
						CpuShares: testCopyUint64(2),
						CpuQuota:  testCopyInt64(3),
						CpuPeriod: testCopyUint64(4),
					},
					isTrackedCCC: true,
					rc: &ResourceConfig{
						CpusetCpus: testCopyString(cpuset.NewCPUSet(1).String()),
						CpuShares:  testCopyUint64(2),
						CpuQuota:   testCopyInt64(3),
						CpuPeriod:  testCopyUint64(4),
					},
				},
			},
		},
		{
			description: fmt.Sprintf("Success, 2 pods in PolicyManager, complex returns"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
					string(podArray[1].pod.UID): podArray[1].pod,
				},
			}),
			mockMap: map[*v1.Pod]*mockStruct{
				podArray[0].pod: {
					rcCCS: &ResourceConfig{
						CpusetCpus: testCopyString(cpuset.NewCPUSet(1).String()),
					},
					isTrackedCCS: true,
					rcCCC: &ResourceConfig{
						CpuShares: testCopyUint64(2),
						CpuQuota:  testCopyInt64(3),
						CpuPeriod: testCopyUint64(4),
					},
					isTrackedCCC: true,
					rc: &ResourceConfig{
						CpusetCpus: testCopyString(cpuset.NewCPUSet(1).String()),
						CpuShares:  testCopyUint64(2),
						CpuQuota:   testCopyInt64(3),
						CpuPeriod:  testCopyUint64(4),
					},
				},
				podArray[1].pod: {
					rcCCS: &ResourceConfig{
						CpusetCpus: testCopyString(cpuset.NewCPUSet(11).String()),
					},
					isTrackedCCS: true,
					rcCCC: &ResourceConfig{
						CpuShares: testCopyUint64(12),
						CpuQuota:  testCopyInt64(13),
						CpuPeriod: testCopyUint64(14),
					},
					isTrackedCCC: true,
					rc: &ResourceConfig{
						CpusetCpus: testCopyString(cpuset.NewCPUSet(11).String()),
						CpuShares:  testCopyUint64(12),
						CpuQuota:   testCopyInt64(13),
						CpuPeriod:  testCopyUint64(14),
					},
				},
			},
		},
	}...)

	// Fail cases
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: fmt.Sprintf("Fail, error from CgroupManager.Update"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			mockMap: map[*v1.Pod]*mockStruct{
				podArray[0].pod: {
					rcCCS:        &ResourceConfig{},
					isTrackedCCS: true,
					rcCCC:        &ResourceConfig{},
					isTrackedCCC: true,
					rc:           &ResourceConfig{},
					errCM:        fmt.Errorf("fake error"),
				},
			},
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: fmt.Sprintf("Fail, pod not added to CPUCFS yet"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			mockMap: map[*v1.Pod]*mockStruct{
				podArray[0].pod: {
					rcCCS:        &ResourceConfig{},
					isTrackedCCS: true,
					rcCCC:        &ResourceConfig{},
					isTrackedCCC: false,
					rc:           &ResourceConfig{},
				},
			},
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: fmt.Sprintf("Fail, pod not added to CPUSet yet"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			mockMap: map[*v1.Pod]*mockStruct{
				podArray[0].pod: {
					rcCCS:        &ResourceConfig{},
					isTrackedCCS: false,
					rcCCC:        &ResourceConfig{},
					isTrackedCCC: true,
					rc:           &ResourceConfig{},
				},
			},
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: fmt.Sprintf("Fail, pod not added neither CPUCFS nor CPUSet yet"),
			pm: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					string(podArray[0].pod.UID): podArray[0].pod,
				},
			}),
			mockMap: map[*v1.Pod]*mockStruct{
				podArray[0].pod: {
					rcCCS:        &ResourceConfig{},
					isTrackedCCS: false,
					rcCCC:        &ResourceConfig{},
					isTrackedCCC: false,
					rc:           &ResourceConfig{},
				},
			},
			expErr: fmt.Errorf("fake error"),
		},
	}...)

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			pm := tc.pm
			pcmMock := new(MockPodContainerManager)
			pm.newPodContainerManager = func() PodContainerManager {
				return pcmMock
			}
			cccMock := pm.cgroupCPUCFS.(*MockCgroup)
			ccsMock := pm.cgroupCPUSet.(*MockCgroup)
			cmMock := pm.cgroupManager.(*MockCgroupManager)
			for pod, m := range tc.mockMap {
				ccsMock.On("ReadPod", pod).
					Return(m.rcCCS, m.isTrackedCCS).
					Once()
				cccMock.On("ReadPod", pod).
					Return(m.rcCCC, m.isTrackedCCC).
					Once()
				cgroupName := testGenerateCgroupName(pod)
				pcmMock.On("GetPodContainerName", pod).
					Return(cgroupName, "").
					Once()
				cgroupConfig := &CgroupConfig{
					Name:               cgroupName,
					ResourceParameters: m.rc,
				}
				cmMock.On("Update", cgroupConfig).
					Return(m.errCM).
					Once()
			}

			err := pm.updateToHost()

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			pcmMock.AssertExpectations(t)
			cccMock.AssertExpectations(t)
			ccsMock.AssertExpectations(t)
			cmMock.AssertExpectations(t)
		})
	}

}
