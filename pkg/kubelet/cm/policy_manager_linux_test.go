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

	"github.com/imdario/mergo"
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
// and details of its implementation is broken down to be tested below,
// for .addPodCgroup and .updateToHost
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
	var testCaseArray []testCaseStruct

	// For simple pod validation
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: "Fail, pod not existed",
			pmBefore:    testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			pmAfter:     testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description: "Fail, pod already exist",
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					"1": testGeneratePodUIDAndPolicy("1", policyDefault),
				},
			}),
			pod: testGeneratePodUIDAndPolicy("1", policyDefault),
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					"1": testGeneratePodUIDAndPolicy("1", policyDefault),
				},
			}),
			expErr: fmt.Errorf("fake error"),
		},
	}...)

	// For unknown policy
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: "Fail, policy unknown, single existing pod",
			pmBefore:    testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			pod:         testGeneratePodUIDAndPolicy("1", policyUnknown),
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					"1": testGeneratePodUIDAndPolicy("1", policyUnknown),
				},
			}),
			expErr: fmt.Errorf("fake error"),
		},
		{
			description: "Fail, policy unknown, multiple existing pods",
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					"2": testGeneratePodUIDAndPolicy("2", policyDefault),
					"3": testGeneratePodUIDAndPolicy("3", policyCPUCFS),
					"4": testGeneratePodUIDAndPolicy("4", policyIsolated),
				},
			}),
			pod: testGeneratePodUIDAndPolicy("1", policyUnknown),
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					"1": testGeneratePodUIDAndPolicy("1", policyUnknown),
					"2": testGeneratePodUIDAndPolicy("2", policyDefault),
					"3": testGeneratePodUIDAndPolicy("3", policyCPUCFS),
					"4": testGeneratePodUIDAndPolicy("4", policyIsolated),
				},
			}),
			expErr: fmt.Errorf("fake error"),
		},
	}...)

	// When no pod exist, the first pod is added
	// Each of the pod will be added as the first pod
	{
		pmBefore := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod := podArray[0].pod
		pmAfter := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		// Host-global Cgroups is initialized with default one
		ccsFake := pmAfter.cgroupCPUSet.(*cgroupCPUSet)
		cpusReservedFake := ccsFake.cpusReserved.String()
		cpusSharedFake := ccsFake.cpusShared.String()
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[0].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
			},
			pmAfter: pmAfter,
		})

		pmBefore = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod = podArray[1].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[1].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
			},
			pmAfter: pmAfter,
		})

		pmBefore = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod = podArray[2].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[2].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpuset.NewCPUSet(1).String()),
				},
			},
			pmAfter: pmAfter,
		})

		pmBefore = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod = podArray[3].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[3].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[3].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSharesMin),
				},
			},
			pmAfter: pmAfter,
		})

		pmBefore = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod = podArray[4].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[4].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[4].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuSmallQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
			},
			pmAfter: pmAfter,
		})

		pmBefore = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod = podArray[5].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding first pod (%q)",
				podArray[5].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[5].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
					CpuShares:  testCopyUint64(cpuSmallShare),
					CpuQuota:   testCopyInt64(cpuLargeQuota),
					CpuPeriod:  testCopyUint64(cpuPeriodDefault),
				},
			},
			pmAfter: pmAfter,
		})
	}

	// When pods are added accumulated, from the same podArray above
	{
		// Adding sampleArray[0].pod,
		// (policyDefault, "1", metav1.NamespaceDefault, "", "")
		pmBefore := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod := podArray[0].pod
		pmAfter := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		// Host-global Cgroups is initialized with default one
		ccsFake := pmAfter.cgroupCPUSet.(*cgroupCPUSet)
		cpusReservedFake := ccsFake.cpusReserved.String()
		cpusSharedFake := ccsFake.cpusShared.String()
		mockMap := map[*v1.Pod]*ResourceConfig{
			podArray[0].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
			},
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[0].pod (%q)",
				podArray[0].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
		})

		// Accumulately adding sampleArray[1].pod
		// (policyDefault, "2", metav1.NamespaceSystem, "", "")
		pmBefore = pmAfter
		pod = podArray[1].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		mockMap = map[*v1.Pod]*ResourceConfig{
			podArray[0].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
			},
			podArray[1].pod: {
				CpusetCpus: testCopyString(cpusReservedFake),
			},
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[1].pod (%q)",
				podArray[1].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
		})

		// Accumulately adding sampleArray[2].pod
		// (policyIsolated, "3", metav1.NamespaceDefault, "1", "1")
		pmBefore = pmAfter
		pod = podArray[2].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		// Update host-global Cgroup
		ccsFake = pmAfter.cgroupCPUSet.(*cgroupCPUSet)
		cpusDedicatedFake := ccsFake.podToCPUS[string(podArray[2].pod.UID)].String()
		cpusReservedFake = ccsFake.cpusReserved.String()
		cpusSharedFake = ccsFake.cpusShared.String()
		mockMap = map[*v1.Pod]*ResourceConfig{
			podArray[0].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
			},
			podArray[1].pod: {
				CpusetCpus: testCopyString(cpusReservedFake),
			},
			podArray[2].pod: {
				CpusetCpus: testCopyString(cpusDedicatedFake),
			},
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[2].pod (%q)",
				podArray[2].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
		})

		// Accumulately adding sampleArray[3].pod
		// (policyCPUCFS, "4", metav1.NamespaceDefault, "", "")
		pmBefore = pmAfter
		pod = podArray[3].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		mockMap = map[*v1.Pod]*ResourceConfig{
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
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[3].pod (%q)",
				podArray[3].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
		})

		// Accumulately adding sampleArray[4].pod
		// (policyCPUCFS, "5", metav1.NamespaceDefault, cpuSmall, cpuSmall)
		pmBefore = pmAfter
		pod = podArray[4].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		mockMap = map[*v1.Pod]*ResourceConfig{
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
			},
			podArray[4].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
				CpuShares:  testCopyUint64(cpuSmallShare),
				CpuQuota:   testCopyInt64(cpuSmallQuota),
				CpuPeriod:  testCopyUint64(cpuPeriodDefault),
			},
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[4].pod (%q)",
				podArray[4].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
		})

		// Accumulately adding sampleArray[5].pod
		// (policyCPUCFS, "6", metav1.NamespaceDefault, cpuSmall, cpuLarge)
		pmBefore = pmAfter
		pod = podArray[5].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		mockMap = map[*v1.Pod]*ResourceConfig{
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
			},
			podArray[4].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
			},
			podArray[5].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
				CpuShares:  testCopyUint64(cpuSmallShare),
				CpuQuota:   testCopyInt64(cpuLargeQuota),
				CpuPeriod:  testCopyUint64(cpuPeriodDefault),
			},
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, adding sampleArray[5].pod (%q)",
				podArray[5].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
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

func TestPolicyManagerImplAddPodAllCgroup(t *testing.T) {
	// The construction of test table is completed by categories below
	type testCaseStruct struct {
		description        string
		isDependencyCalled bool
		pmBefore           *policyManagerImpl
		pod                *v1.Pod
		pmAfter            *policyManagerImpl
		errCPUCFSAddPod    error
		errCPUSetAddPod    error
		expErr             error
	}
	var testCaseArray []testCaseStruct

	// For all supported policies
	policyMap := map[string]struct {
		uidString string
		pod       *v1.Pod
	}{
		policyDefault: {
			uidString: "1",
			pod:       testGeneratePodUIDAndPolicy("1", policyDefault),
		},
		policyCPUCFS: {
			uidString: "2",
			pod:       testGeneratePodUIDAndPolicy("2", policyCPUCFS),
		},
		policyIsolated: {
			uidString: "3",
			pod:       testGeneratePodUIDAndPolicy("3", policyIsolated),
		},
	}
	errCCSAArray := []error{nil, fmt.Errorf("fake error")}
	errCCCAArray := []error{nil, fmt.Errorf("fake error")}
	// For these policies, pod is added to all Cgroup
	for policy, value := range policyMap {
		// For possible error from Cgroup.AddPod()
		for _, errCCSA := range errCCSAArray {
			for _, errCCCA := range errCCCAArray {
				if errCCCA == nil && errCCSA == nil {
					continue
				}

				testCaseArray = append(testCaseArray, testCaseStruct{
					description: fmt.Sprintf("Fail, for policy (%q), with errCPUSetAddPod (%q), errCPUCFSAddPod (%q)",
						policy, errCCCA, errCCSA),
					isDependencyCalled: true,
					pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
						uidToPod: map[string]*v1.Pod{
							value.uidString: value.pod,
						},
					}),
					pod: value.pod,
					pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
						uidToPod: map[string]*v1.Pod{
							value.uidString: value.pod,
						},
					}),
					errCPUCFSAddPod: errCCCA,
					errCPUSetAddPod: errCCSA,
					expErr:          fmt.Errorf("fake error"),
				})
			}
		}
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			pm := tc.pmBefore
			ccsMock := pm.cgroupCPUSet.(*MockCgroup)
			cccMock := pm.cgroupCPUCFS.(*MockCgroup)
			if tc.isDependencyCalled {
				ccsMock.On("AddPod", tc.pod).
					Return(tc.errCPUSetAddPod).Once()
				cccMock.On("AddPod", tc.pod).
					Return(tc.errCPUCFSAddPod).Once()
			}

			err := pm.addPodAllCgroup(tc.pod)

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

func TestPolicyManagerImplRemovePod(t *testing.T) {
	type testCaseStruct struct {
		description string
		pmBefore    *policyManagerImpl
		pod         *v1.Pod
		mockMap     map[*v1.Pod]*ResourceConfig
		pmAfter     *policyManagerImpl
		expErr      error
	}
	var testCaseArray []testCaseStruct

	// For simple pod validation
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: "Fail, pod not existed",
			pmBefore:    testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			pmAfter:     testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description: "Fail, pod not added yet",
			pmBefore:    testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			pod:         testGeneratePodUIDAndPolicy("1", policyDefault),
			pmAfter:     testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			expErr:      fmt.Errorf("fake error"),
		},
	}...)

	// For unknown policy
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description: "Fail, policy unknown, single existing pod",
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					"1": testGeneratePodUIDAndPolicy("1", policyUnknown),
				},
			}),
			pod:     testGeneratePodUIDAndPolicy("1", policyUnknown),
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			expErr:  fmt.Errorf("fake error"),
		},
		{
			description: "Fail, policy unknown, multiple existing pods",
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					"1": testGeneratePodUIDAndPolicy("1", policyUnknown),
					"2": testGeneratePodUIDAndPolicy("2", policyDefault),
					"3": testGeneratePodUIDAndPolicy("3", policyCPUCFS),
					"4": testGeneratePodUIDAndPolicy("4", policyIsolated),
				},
			}),
			pod: testGeneratePodUIDAndPolicy("1", policyUnknown),
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					"2": testGeneratePodUIDAndPolicy("2", policyDefault),
					"3": testGeneratePodUIDAndPolicy("3", policyCPUCFS),
					"4": testGeneratePodUIDAndPolicy("4", policyIsolated),
				},
			}),
			expErr: fmt.Errorf("fake error"),
		},
	}...)

	// When the only existing pod is removed
	// Each of the pod will be removed as the only existing pod
	// The data is the exact the reverse of TestPolicyManagerAddPod() above
	{
		pmBefore := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod := podArray[0].pod
		pmAfter := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		// Host-global Cgroups is initialized with default one
		ccsFake := pmAfter.cgroupCPUSet.(*cgroupCPUSet)
		cpusReservedFake := ccsFake.cpusReserved.String()
		cpusSharedFake := ccsFake.cpusShared.String()
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[0].description),
			pmBefore: pmAfter,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[0].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
			},
			pmAfter: pmBefore,
		})

		pmBefore = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod = podArray[1].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[1].description),
			pmBefore: pmAfter,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[1].pod: {
					CpusetCpus: testCopyString(cpusReservedFake),
				},
			},
			pmAfter: pmBefore,
		})

		pmBefore = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod = podArray[2].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[2].description),
			pmBefore: pmAfter,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[2].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
			},
			pmAfter: pmBefore,
		})

		pmBefore = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod = podArray[3].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[3].description),
			pmBefore: pmAfter,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[3].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
			},
			pmAfter: pmBefore,
		})

		pmBefore = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod = podArray[4].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[4].description),
			pmBefore: pmAfter,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[4].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
			},
			pmAfter: pmBefore,
		})

		pmBefore = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		pod = podArray[5].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing the only pod (%q)",
				podArray[5].description),
			pmBefore: pmAfter,
			pod:      pod,
			mockMap: map[*v1.Pod]*ResourceConfig{
				podArray[5].pod: {
					CpusetCpus: testCopyString(cpusSharedFake),
				},
			},
			pmAfter: pmBefore,
		})
	}

	// When pods are removed accumulated, from the same podArray above
	// This process is exact the reverse order of TestPolicyManagerAddPod() above
	{
		// Accumulately removing sampleArray[5].pod
		// (policyCPUCFS, "6", metav1.NamespaceDefault, cpuSmall, cpuLarge)
		pmBefore := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		pod := podArray[5].pod
		pmAfter := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		// Update host-global Cgroup
		// cpusDedicatedFake := cpuset.NewCPUSet(1).String()
		ccsFake := pmAfter.cgroupCPUSet.(*cgroupCPUSet)
		cpusDedicatedFake := ccsFake.podToCPUS[string(podArray[2].pod.UID)].String()
		cpusReservedFake := ccsFake.cpusReserved.String()
		cpusSharedFake := ccsFake.cpusShared.String()
		mockMap := map[*v1.Pod]*ResourceConfig{
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
			},
			podArray[4].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
			},
			podArray[5].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
			},
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[5].pod (%q)",
				podArray[5].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
		})

		// Accumulately removing sampleArray[4].pod
		// (policyCPUCFS, "5", metav1.NamespaceDefault, cpuSmall, cpuSmall)
		pmBefore = pmAfter
		pod = podArray[4].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		mockMap = map[*v1.Pod]*ResourceConfig{
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
			},
			podArray[4].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
			},
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[4].pod (%q)",
				podArray[4].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
		})

		// Accumulately removing sampleArray[3].pod
		// (policyCPUCFS, "4", metav1.NamespaceDefault, "", "")
		pmBefore = pmAfter
		pod = podArray[3].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		mockMap = map[*v1.Pod]*ResourceConfig{
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
			},
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[3].pod (%q)",
				podArray[3].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
		})

		// Accumulately removing sampleArray[2].pod
		// (policyIsolated, "3", metav1.NamespaceDefault, "1", "1")
		pmBefore = pmAfter
		pod = podArray[2].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		// Update host-global Cgroup
		ccsFake = pmAfter.cgroupCPUSet.(*cgroupCPUSet)
		cpusReservedFake = ccsFake.cpusReserved.String()
		cpusSharedFake = ccsFake.cpusShared.String()
		mockMap = map[*v1.Pod]*ResourceConfig{
			podArray[0].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
			},
			podArray[1].pod: {
				CpusetCpus: testCopyString(cpusReservedFake),
			},
			podArray[2].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
			},
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[2].pod (%q)",
				podArray[2].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
		})

		// Accumulately removing sampleArray[1].pod
		// (policyDefault, "2", metav1.NamespaceSystem, "", "")
		pmBefore = pmAfter
		pod = podArray[1].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
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
		})
		mockMap = map[*v1.Pod]*ResourceConfig{
			podArray[0].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
			},
			podArray[1].pod: {
				CpusetCpus: testCopyString(cpusReservedFake),
			},
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[1].pod (%q)",
				podArray[1].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
		})

		// Removing sampleArray[0].pod,
		// (policyDefault, "1", metav1.NamespaceDefault, "", "")
		pmBefore = pmAfter
		pod = podArray[0].pod
		pmAfter = testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
			uidToPod:     map[string]*v1.Pod{},
			cgroupCPUCFS: testGenerateCgroupCPUCFS(&testCgroupCPUCFS{}),
			cgroupCPUSet: testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
		})
		mockMap = map[*v1.Pod]*ResourceConfig{
			podArray[0].pod: {
				CpusetCpus: testCopyString(cpusSharedFake),
			},
		}
		testCaseArray = append(testCaseArray, testCaseStruct{
			description: fmt.Sprintf("Success, removing sampleArray[0].pod (%q)",
				podArray[0].description),
			pmBefore: pmBefore,
			pod:      pod,
			mockMap:  mockMap,
			pmAfter:  pmAfter,
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

func TestPolicyManagerImplRemovePodAllCgroup(t *testing.T) {
	type testCaseStruct struct {
		description        string
		isDependencyCalled bool
		pmBefore           *policyManagerImpl
		podUID             string
		pmAfter            *policyManagerImpl
		errCPUSetRemovePod error
		errCPUCFSRemovePod error
		expErr             error
	}
	var testCaseArray []testCaseStruct

	// For all supported policies
	policyMap := map[string]struct {
		uidString string
	}{
		policyDefault: {
			uidString: "1",
		},
		policyCPUCFS: {
			uidString: "2",
		},
		policyIsolated: {
			uidString: "3",
		},
	}
	errCCSRArray := []error{nil, fmt.Errorf("fake error")}
	errCCCRArray := []error{nil, fmt.Errorf("fake error")}
	// For these policies, pod is removed from all Cgroup
	for policy, value := range policyMap {
		// For possible error from Cgroup.RemovePod()
		for _, errCCSR := range errCCSRArray {
			for _, errCCCR := range errCCCRArray {
				if errCCCR == nil && errCCSR == nil {
					continue
				}

				testCaseArray = append(testCaseArray, testCaseStruct{
					description: fmt.Sprintf("Fail, for policy (%q), with errCPUSetRemovePod (%q), errCPUCFSRemovePod (%q)",
						policy, errCCCR, errCCSR),
					isDependencyCalled: true,
					pmBefore:           testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
					podUID:             value.uidString,
					pmAfter:            testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
					errCPUCFSRemovePod: errCCCR,
					errCPUSetRemovePod: errCCSR,
					expErr:             fmt.Errorf("fake error"),
				})
			}
		}
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			pm := tc.pmBefore
			ccsMock := pm.cgroupCPUSet.(*MockCgroup)
			cccMock := pm.cgroupCPUCFS.(*MockCgroup)
			if tc.isDependencyCalled {
				ccsMock.On("RemovePod", tc.podUID).
					Return(tc.errCPUSetRemovePod).Once()
				cccMock.On("RemovePod", tc.podUID).
					Return(tc.errCPUCFSRemovePod).Once()
			}

			err := pm.removeAllCgroup(tc.podUID)

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
		rcCCS *ResourceConfig
		cc    *CgroupConfig
		errCM error
	}
	type testCaseStruct struct {
		description string
		pm          *policyManagerImpl
		podUpdated  *v1.Pod
		isAdded     bool
		mockMap     map[*v1.Pod]*mockStruct
		rcCCC       *ResourceConfig
		isTracked   bool
		expErr      error
	}
	var testCaseArray []testCaseStruct

	// Set up all pods currently running in the host
	podDetailArray := []struct {
		pod             *v1.Pod
		rcCCCIsTracked  *ResourceConfig
		rcCCCNotTracked *ResourceConfig
		rcCCSIsTracked  *ResourceConfig
		rcAllIsTracked  *ResourceConfig
		rcCCSNotTracked *ResourceConfig
		rcAllNotTracked *ResourceConfig
	}{
		{
			pod:             testGeneratePodWithUID("1"),
			rcCCCIsTracked:  &ResourceConfig{},
			rcCCCNotTracked: &ResourceConfig{},
			rcCCSIsTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(1).String()),
			},
			rcAllIsTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(1).String()),
			},
			rcCCSNotTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(11, 12).String()),
			},
			rcAllNotTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(11, 12).String()),
			},
		},
		{
			pod: testGeneratePodWithUID("2"),
			rcCCCIsTracked: &ResourceConfig{
				CpuShares: testCopyUint64(21),
			},
			rcCCCNotTracked: &ResourceConfig{},
			rcCCSIsTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(2).String()),
			},
			rcAllIsTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(2).String()),
			},
			rcCCSNotTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(21).String()),
			},
			rcAllNotTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(21).String()),
			},
		},
		{
			pod: testGeneratePodWithUID("3"),
			rcCCCIsTracked: &ResourceConfig{
				CpuShares: testCopyUint64(31),
				CpuQuota:  testCopyInt64(32),
				CpuPeriod: testCopyUint64(33),
			},
			rcCCCNotTracked: &ResourceConfig{},
			rcCCSIsTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(31, 32, 33).String()),
			},
			rcAllIsTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(31, 32, 33).String()),
			},
			rcCCSNotTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(1, 2, 3).String()),
			},
			rcAllNotTracked: &ResourceConfig{
				CpusetCpus: testCopyString(cpuset.NewCPUSet(1, 2, 3).String()),
			},
		},
	}

	pm := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
		uidToPod: make(map[string]*v1.Pod),
	})
	for _, pd := range podDetailArray {
		pm.uidToPod[string(pd.pod.UID)] = pd.pod
	}

	// For pods of different policies, after AddPod/RemovePod()
	for _, pdUpdated := range podDetailArray {
		isAddedArray := []bool{true, false}
		isTrackedArray := []bool{true, false}
		for _, isAdded := range isAddedArray {
			for _, isTracked := range isTrackedArray {
				expErr := fmt.Errorf("fake error")
				if isAdded == isTracked {
					expErr = nil
				}
				// Host-global Cgroup for all pods
				mockMap := make(map[*v1.Pod]*mockStruct)
				for _, pd := range podDetailArray {
					var rcAll *ResourceConfig
					var rcCCS *ResourceConfig
					// When the pod is just being added, or removed
					if isAdded {
						rcCCS = pd.rcCCSIsTracked
						rcAll = pd.rcAllIsTracked
					} else {
						rcCCS = pd.rcCCSNotTracked
						rcAll = pd.rcAllNotTracked
					}
					mockMap[pd.pod] = &mockStruct{
						rcCCS: rcCCS,
						cc: &CgroupConfig{
							Name:               testGenerateCgroupName(pd.pod),
							ResourceParameters: rcAll,
						},
					}
				}

				// Pod-local Cgroup of only the current pod is added to the result.
				var rcCCC *ResourceConfig
				if isAdded {
					rcCCC = pdUpdated.rcCCCIsTracked
				} else {
					rcCCC = pdUpdated.rcCCCNotTracked
				}
				// Make sure original rcCCC is not modified for other iterations
				rcUpdated := &ResourceConfig{}
				mergo.Merge(rcUpdated, mockMap[pdUpdated.pod].cc.ResourceParameters)
				mergo.Merge(rcUpdated, rcCCC)
				mockMap[pdUpdated.pod].cc.ResourceParameters = rcUpdated

				testCaseArray = append(testCaseArray, []testCaseStruct{
					{
						description: fmt.Sprintf("For policy (%q), Complete with error (%v), for isAdded (%v), isTracked (%v)",
							pdUpdated.pod.Spec.Policy, expErr, isTracked, isAdded),
						pm:         pm,
						podUpdated: pdUpdated.pod,
						isAdded:    isAdded,
						rcCCC:      rcCCC,
						isTracked:  isTracked,
						mockMap:    mockMap,
						expErr:     expErr,
					},
				}...)
			}
		}
	}

	// When fails for error from CgroupManager.Update() for any pod fails,
	// simple setup after AddPod()
	for _, pdUpdated := range podDetailArray {
		// Any pods may got error calling CgroupManager.Update()
		for _, pd := range podDetailArray {
			// Host-global Cgroup for all pods
			mockMap := make(map[*v1.Pod]*mockStruct)
			for _, pd := range podDetailArray {
				mockMap[pd.pod] = &mockStruct{
					rcCCS: pd.rcCCSIsTracked,
					cc: &CgroupConfig{
						Name:               testGenerateCgroupName(pd.pod),
						ResourceParameters: pd.rcAllIsTracked,
					},
				}
			}
			// Pod-local Cgroup of only the current pod is added to the result.
			// Make sure original rcCCC is not modified for other iterations
			rcUpdated := &ResourceConfig{}
			mergo.Merge(rcUpdated, mockMap[pdUpdated.pod].cc.ResourceParameters)
			mergo.Merge(rcUpdated, pdUpdated.rcCCCIsTracked)
			mockMap[pdUpdated.pod].cc.ResourceParameters = rcUpdated

			// Set up error
			mockMap[pd.pod].errCM = fmt.Errorf("fake error")

			testCaseArray = append(testCaseArray, []testCaseStruct{
				{
					description: fmt.Sprintf("Fail, for pod (%q), got error from pod (%q) calling CgroupManager.Update()",
						pdUpdated.pod.UID, pd.pod.UID),
					pm:         pm,
					podUpdated: pdUpdated.pod,
					isAdded:    true,
					rcCCC:      pdUpdated.rcCCCIsTracked,
					isTracked:  true,
					mockMap:    mockMap,
					expErr:     fmt.Errorf("fake error"),
				},
			}...)
		}
	}

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
			activePods := []*v1.Pod{}
			for pod, m := range tc.mockMap {
				activePods = append(activePods, pod)
				pcmMock.On("GetPodContainerName", pod).
					Return(testGenerateCgroupName(pod), "").
					Once()
				cmMock.On("Update", m.cc).
					Return(m.errCM).
					Once()
				ccsMock.On("ReadPod", pod).
					Return(m.rcCCS, tc.isTracked).
					Once()
			}
			pm.activePodsFunc = func() []*v1.Pod {
				return activePods
			}
			cccMock.On("ReadPod", tc.podUpdated).
				Return(tc.rcCCC, tc.isTracked).
				Once()

			err := pm.updateToHost(tc.podUpdated, tc.isAdded)

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
