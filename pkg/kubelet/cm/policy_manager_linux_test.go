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
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager"
	cputopology "k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
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
func testEqualPolicyManager(t *testing.T,
	expect *policyManagerImpl, actual *policyManagerImpl) {
	assert.Equal(t, expect.uidToPod, actual.uidToPod)
}

func TestNewPolicyManagerAndStart(t *testing.T) {
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
				testEqualPolicyManager(t,
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
func TestPolicyManagerAddPod(t *testing.T) {
	// The construction of test table is completed by categories below
	type testCaseStruct struct {
		description        string
		isDependencyCalled bool
		pmBefore           *policyManagerImpl
		pod                *v1.Pod
		pmAfter            *policyManagerImpl
		// errCPUCFSAddPod    error
		// errCPUSetAddPod    error
		// rcCCC              *ResourceConfig
		// isTracked          bool
		expErr error
	}
	var testCaseArray []testCaseStruct

	// For simple pod validation
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description:        "Fail, pod not existed",
			isDependencyCalled: false,
			pmBefore:           testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			pmAfter:            testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			expErr:             fmt.Errorf("fake error"),
		},
		{
			description:        "Fail, pod already exist",
			isDependencyCalled: false,
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
			description:        "Fail, policy unknown, single existing pod",
			isDependencyCalled: false,
			pmBefore:           testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			pod:                testGeneratePodUIDAndPolicy("1", policyUnknown),
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					"1": testGeneratePodUIDAndPolicy("1", policyUnknown),
				},
			}),
			expErr: fmt.Errorf("fake error"),
		},
		{
			description:        "Fail, policy unknown, multiple existing pods",
			isDependencyCalled: false,
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

	// // For these policies, pod is added to all Cgroup
	// reasonArray := []string{policyDefault, policyCPUCFS, policyIsolated}
	// uidArray := []string{testUIDPolicyDefault, testUIDPolicyCPUCFS, testUIDPolicyIsolated}
	// podArray := []*v1.Pod{testPodPolicyDefault, testPodPolicyCPUCFS, testPodPolicyIsolated}
	// rcCCCArray := []*ResourceConfig{testRCCCCPolicyDefault, testRCCCCPolicyCPUCFS, testRCCCCPolicyIsolated}
	// for i, reason := range reasonArray {
	// 	testCaseArray = append(testCaseArray, testCaseStruct{
	// 		description:        fmt.Sprintf("Success, simple for policy (%q)", reason),
	// 		isDependencyCalled: true,
	// 		pmBefore:           testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
	// 		pod:                podArray[i],
	// 		pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
	// 			uidToPod: map[string]*v1.Pod{
	// 				uidArray[i]: podArray[i],
	// 			},
	// 		}),
	// 		rcCCC:     rcCCCArray[i],
	// 		isTracked: true,
	// 	})
	// }

	// // For multiple existing pods
	// testCaseArray = append(testCaseArray, []testCaseStruct{
	// 	{
	// 		description:        "Success, multiple existing pods",
	// 		isDependencyCalled: true,
	// 		pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
	// 			uidToPod: map[string]*v1.Pod{
	// 				testUIDPolicyUnknown:  testPodPolicyUnknown,
	// 				testUIDPolicyCPUCFS:   testPodPolicyCPUCFS,
	// 				testUIDPolicyIsolated: testPodPolicyIsolated,
	// 			},
	// 		}),
	// 		pod: testPodPolicyDefault,
	// 		pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
	// 			uidToPod: map[string]*v1.Pod{
	// 				testUIDPolicyUnknown:  testPodPolicyUnknown,
	// 				testUIDPolicyDefault:  testPodPolicyDefault,
	// 				testUIDPolicyCPUCFS:   testPodPolicyCPUCFS,
	// 				testUIDPolicyIsolated: testPodPolicyIsolated,
	// 			},
	// 		}),
	// 		rcCCC:     testRCCCCPolicyDefault,
	// 		isTracked: true,
	// 	},
	// }...)

	// testCaseArray is built by categories above
	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			pm := tc.pmBefore
			// ccsMock := pm.cgroupCPUSet.(*MockCgroup)
			// cccMock := pm.cgroupCPUCFS.(*MockCgroup)
			// if tc.isDependencyCalled {
			// 	ccsMock.On("AddPod", tc.pod).
			// 		Return(tc.errCPUSetAddPod).Once()
			// 	cccMock.On("AddPod", tc.pod).
			// 		Return(tc.errCPUCFSAddPod).Once()
			// 	cccMock.On("ReadPod", string(tc.pod.UID)).
			// 		Return(tc.rcCCC, tc.isTracked).Once()
			// }

			err := pm.AddPod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			testEqualPolicyManager(t, tc.pmAfter, pm)
			// ccsMock.AssertExpectations(t)
			// cccMock.AssertExpectations(t)
		})
	}
}

func TestPolicyManagerAddPodAllCgroup(t *testing.T) {
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
			testEqualPolicyManager(t, tc.pmAfter, pm)
			ccsMock.AssertExpectations(t)
			cccMock.AssertExpectations(t)
		})
	}
}

func TestPolicyManagerRemovePod(t *testing.T) {
	type testCaseStruct struct {
		description        string
		isDependencyCalled bool
		pmBefore           *policyManagerImpl
		pod                *v1.Pod
		pmAfter            *policyManagerImpl
		// errCPUSetRemovePod error
		// errCPUCFSRemovePod error
		// rcCCC              *ResourceConfig
		// isTracked          bool
		expErr error
	}
	var testCaseArray []testCaseStruct

	// For simple pod validation
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description:        "Fail, pod not existed",
			isDependencyCalled: false,
			pmBefore:           testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			pmAfter:            testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			expErr:             fmt.Errorf("fake error"),
		},
		{
			description:        "Fail, pod not added yet",
			isDependencyCalled: false,
			pmBefore:           testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			pod:                testGeneratePodUIDAndPolicy("1", policyDefault),
			pmAfter:            testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			expErr:             fmt.Errorf("fake error"),
		},
	}...)

	// For unknown policy
	testCaseArray = append(testCaseArray, []testCaseStruct{
		{
			description:        "Fail, policy unknown, single existing pod",
			isDependencyCalled: false,
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
			description:        "Fail, policy unknown, multiple existing pods",
			isDependencyCalled: false,
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

	// // For these policies, pod is removed from all Cgroup
	// reasonArray := []string{policyDefault, policyCPUCFS, policyIsolated}
	// uidArray := []string{testUIDPolicyDefault, testUIDPolicyCPUCFS, testUIDPolicyIsolated}
	// podArray := []*v1.Pod{testPodPolicyDefault, testPodPolicyCPUCFS, testPodPolicyIsolated}
	// rcCCCArray := []*ResourceConfig{testRCCCCPolicyDefault, testRCCCCPolicyCPUCFS, testRCCCCPolicyIsolated}
	// for i, reason := range reasonArray {
	// 	testCaseArray = append(testCaseArray, testCaseStruct{
	// 		description:        fmt.Sprintf("Success, simple for policy (%q)", reason),
	// 		isDependencyCalled: true,
	// 		pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
	// 			uidToPod: map[string]*v1.Pod{
	// 				uidArray[i]: podArray[i],
	// 			},
	// 		}),
	// 		pod:       podArray[i],
	// 		pmAfter:   testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
	// 		rcCCC:     rcCCCArray[i],
	// 		isTracked: false,
	// 	})
	// }

	// // Successfully remove existing pod from tracked pods
	// testCaseArray = append(testCaseArray, []testCaseStruct{
	// 	{
	// 		description:        "Success, multiple existing pod",
	// 		isDependencyCalled: true,
	// 		pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
	// 			uidToPod: map[string]*v1.Pod{
	// 				testUIDPolicyUnknown:  testPodPolicyUnknown,
	// 				testUIDPolicyDefault:  testPodPolicyDefault,
	// 				testUIDPolicyCPUCFS:   testPodPolicyCPUCFS,
	// 				testUIDPolicyIsolated: testPodPolicyIsolated,
	// 			},
	// 		}),
	// 		pod: testPodPolicyDefault,
	// 		pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
	// 			uidToPod: map[string]*v1.Pod{
	// 				testUIDPolicyUnknown:  testPodPolicyUnknown,
	// 				testUIDPolicyCPUCFS:   testPodPolicyCPUCFS,
	// 				testUIDPolicyIsolated: testPodPolicyIsolated,
	// 			},
	// 		}),
	// 		rcCCC:     testRCCCCPolicyDefault,
	// 		isTracked: false,
	// 	},
	// }...)

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			pm := tc.pmBefore
			// ccsMock := pm.cgroupCPUSet.(*MockCgroup)
			// cccMock := pm.cgroupCPUCFS.(*MockCgroup)
			// if tc.isDependencyCalled {
			// 	ccsMock.On("RemovePod", string(tc.pod.UID)).
			// 		Return(tc.errCPUSetRemovePod).Once()
			// 	cccMock.On("RemovePod", string(tc.pod.UID)).
			// 		Return(tc.errCPUCFSRemovePod).Once()
			// 	cccMock.On("ReadPod", string(tc.pod.UID)).
			// 		Return(tc.rcCCC, tc.isTracked).Once()
			// }

			err := pm.RemovePod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			testEqualPolicyManager(t, tc.pmAfter, pm)
			// ccsMock.AssertExpectations(t)
			// cccMock.AssertExpectations(t)
		})
	}
}

func TestPolicyManagerRemovePodAllCgroup(t *testing.T) {
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
			testEqualPolicyManager(t, tc.pmAfter, pm)
			ccsMock.AssertExpectations(t)
			cccMock.AssertExpectations(t)
		})
	}
}

func TestPolicyManagerUpdateToHost(t *testing.T) {
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
