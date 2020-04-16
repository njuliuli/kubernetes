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
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager"
	cputopology "k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

var (
	// TODO(li) there are not const pointer in Go,
	// how to make sure the values at those pointers are not modified during the test?
	testUIDPolicyDefault = "2"
	testPodPolicyDefault = testGeneratePod(policyDefault,
		testUIDPolicyDefault, "", "")
	testRCCCCPolicyDefault = &ResourceConfig{}

	testUIDPolicyCPUCFS = "3"
	testPodPolicyCPUCFS = testGeneratePod(policyCPUCFS,
		testUIDPolicyCPUCFS, "", "")
	testRCCCCPolicyCPUCFS = &ResourceConfig{
		CpuShares: testCopyUint64(1),
		CpuQuota:  testCopyInt64(2),
		CpuPeriod: testCopyUint64(3),
	}

	testUIDPolicyIsolated = "4"
	testPodPolicyIsolated = testGeneratePod(policyIsolated,
		testUIDPolicyIsolated, "", "")
	testRCCCCPolicyIsolated = &ResourceConfig{}

	// For unknown policy
	testUIDPolicyUnknown = "1"
	testPodPolicyUnknown = testGeneratePod(policyUnknown,
		testUIDPolicyUnknown, "", "")
	// For pod after being removed from Cgroup
	testRCCCCNotTracked = testRCCCCPolicyDefault
)

// Generate pod with given fields set
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
	uidToPod     map[string]*v1.Pod
	cgroupCPUCFS Cgroup
	cgroupCPUSet Cgroup
}

// Generate default policyManagerImpl, with customized default values after initialization
func testGeneratePolicyManagerImpl(tpm *testPolicyManagerImpl) *policyManagerImpl {
	pm := &policyManagerImpl{
		uidToPod:     make(map[string]*v1.Pod),
		cgroupCPUCFS: new(MockCgroup),
		cgroupCPUSet: new(MockCgroup),
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

	return pm
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

			// Action:
			// NewPolicyManager(...) and then PolicyManager.Start(),
			// since they are only executed once and in this order
			newPolicyManager, err := NewPolicyManager(newCgroupCPUCFS, newCgroupCPUSet,
				new(MockCgroupManager), newPodContainerManager,
				cpuTopologyFake, cpusSpecificFake, nodeAllocatableReservationFake)
			if err == nil {
				err = newPolicyManager.Start()
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
					testUIDPolicyDefault: testPodPolicyDefault,
				},
			}),
			pod: testPodPolicyDefault,
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					testUIDPolicyDefault: testPodPolicyDefault,
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
			pod:                testPodPolicyUnknown,
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					testUIDPolicyUnknown: testPodPolicyUnknown,
				},
			}),
			expErr: fmt.Errorf("fake error"),
		},
		{
			description:        "Fail, policy unknown, multiple existing pods",
			isDependencyCalled: false,
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					testUIDPolicyDefault:  testPodPolicyDefault,
					testUIDPolicyCPUCFS:   testPodPolicyCPUCFS,
					testUIDPolicyIsolated: testPodPolicyIsolated,
				},
			}),
			pod: testPodPolicyUnknown,
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					testUIDPolicyUnknown:  testPodPolicyUnknown,
					testUIDPolicyDefault:  testPodPolicyDefault,
					testUIDPolicyCPUCFS:   testPodPolicyCPUCFS,
					testUIDPolicyIsolated: testPodPolicyIsolated,
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

	reasonArray := []string{policyDefault, policyCPUCFS, policyIsolated}
	uidArray := []string{testUIDPolicyDefault, testUIDPolicyCPUCFS, testUIDPolicyIsolated}
	podArray := []*v1.Pod{testPodPolicyDefault, testPodPolicyCPUCFS, testPodPolicyIsolated}
	errCCSAArray := []error{nil, fmt.Errorf("fake error")}
	errCCCAArray := []error{nil, fmt.Errorf("fake error")}
	// For these policies, pod is added to all Cgroup
	for i, reason := range reasonArray {
		// For possible error from Cgroup.AddPod()
		for _, errCCSA := range errCCSAArray {
			for _, errCCCA := range errCCCAArray {
				if errCCCA == nil && errCCSA == nil {
					continue
				}

				testCaseArray = append(testCaseArray, testCaseStruct{
					description: fmt.Sprintf("Fail, for policy (%q), with errCPUSetAddPod (%q), errCPUCFSAddPod (%q)",
						reason, errCCCA, errCCSA),
					isDependencyCalled: true,
					pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
						uidToPod: map[string]*v1.Pod{
							uidArray[i]: podArray[i],
						},
					}),
					pod: podArray[i],
					pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
						uidToPod: map[string]*v1.Pod{
							uidArray[i]: podArray[i],
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
			pod:                testPodPolicyDefault,
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
					testUIDPolicyUnknown: testPodPolicyUnknown,
				},
			}),
			pod:     testPodPolicyUnknown,
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
			expErr:  fmt.Errorf("fake error"),
		},
		{
			description:        "Fail, policy unknown, multiple existing pods",
			isDependencyCalled: false,
			pmBefore: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					testUIDPolicyUnknown:  testPodPolicyUnknown,
					testUIDPolicyDefault:  testPodPolicyDefault,
					testUIDPolicyCPUCFS:   testPodPolicyCPUCFS,
					testUIDPolicyIsolated: testPodPolicyIsolated,
				},
			}),
			pod: testPodPolicyUnknown,
			pmAfter: testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
				uidToPod: map[string]*v1.Pod{
					testUIDPolicyDefault:  testPodPolicyDefault,
					testUIDPolicyCPUCFS:   testPodPolicyCPUCFS,
					testUIDPolicyIsolated: testPodPolicyIsolated,
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

	reasonArray := []string{policyDefault, policyCPUCFS, policyIsolated}
	uidArray := []string{testUIDPolicyDefault, testUIDPolicyCPUCFS, testUIDPolicyIsolated}
	errCCSRArray := []error{nil, fmt.Errorf("fake error")}
	errCCCRArray := []error{nil, fmt.Errorf("fake error")}
	// For these policies, pod is removed from all Cgroup
	for i, reason := range reasonArray {
		// For possible error from Cgroup.RemovePod()
		for _, errCCSR := range errCCSRArray {
			for _, errCCCR := range errCCCRArray {
				if errCCCR == nil && errCCSR == nil {
					continue
				}

				testCaseArray = append(testCaseArray, testCaseStruct{
					description: fmt.Sprintf("Fail, for policy (%q), with errCPUSetRemovePod (%q), errCPUCFSRemovePod (%q)",
						reason, errCCCR, errCCSR),
					isDependencyCalled: true,
					pmBefore:           testGeneratePolicyManagerImpl(&testPolicyManagerImpl{}),
					podUID:             uidArray[i],
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
	type testCaseStruct struct {
		description string
		pm          *policyManagerImpl
		pod         *v1.Pod
		isAdded     bool
		rcCCC       *ResourceConfig
		isTracked   bool
		// cgroupConfig        *CgroupConfig
		// expErrCgroupManager error
		expErr error
	}
	var testCaseArray []testCaseStruct

	// We can only resue this pointer/map when function being test does not modify its value,
	// thus we cannot make it global and used by testing .AddPod()/RemovePod()
	pmFake := testGeneratePolicyManagerImpl(&testPolicyManagerImpl{
		uidToPod: map[string]*v1.Pod{
			testUIDPolicyUnknown:  testPodPolicyUnknown,
			testUIDPolicyDefault:  testPodPolicyDefault,
			testUIDPolicyCPUCFS:   testPodPolicyCPUCFS,
			testUIDPolicyIsolated: testPodPolicyIsolated,
		},
	})

	// For all supported policies,
	// the value is also different depend on whether the pod isAdded
	isAddedArray := []bool{true, false}
	isTrackedArray := []bool{true, false}
	podArray := []*v1.Pod{testPodPolicyDefault, testPodPolicyCPUCFS, testPodPolicyIsolated}
	rcCCCArray := []*ResourceConfig{testRCCCCPolicyDefault, testRCCCCPolicyCPUCFS, testRCCCCPolicyIsolated}
	for i, pod := range podArray {
		for _, isAdded := range isAddedArray {
			for _, isTracked := range isTrackedArray {
				rcCCC := rcCCCArray[i]
				if !isAdded {
					rcCCC = testRCCCCNotTracked
				}
				expErr := fmt.Errorf("fake error")
				if isAdded == isTracked {
					expErr = nil
				}

				testCaseArray = append(testCaseArray, []testCaseStruct{
					{
						description: fmt.Sprintf("Complete with error (%v), for isAdded (%v), isTracked (%v)",
							expErr, isTracked, isAdded),
						pm:        pmFake,
						pod:       pod,
						isAdded:   isAdded,
						rcCCC:     rcCCC,
						isTracked: isTracked,
						expErr:    expErr,
					},
				}...)
			}
		}
	}

	// // No existing pod in cgroupCPUSet
	// cgroupName := CgroupName{"kubepods", "burstable", "pod1234-abcd-5678-efgh"}
	// cgroupPath := "kubepods/burstable/pod1234-abcd-5678-efgh"

	// testCaseArray = append(testCaseArray, []testCaseStruct{
	// 	{
	// 		description: "Fail, error in calling CgroupManager.Update(...)",
	// 		ccsBefore:   testGenerateCgroupCPUSet(&testCgroupCPUSet{}),
	// 		pod:         testGeneratePod(policyIsolated, "1", "1", "1"),
	// 		ccsAfter: testGenerateCgroupCPUSet(&testCgroupCPUSet{
	// 			podSet: sets.NewString("1"),
	// 			cpusShared: testCPUSShared.
	// 				Difference(cpuset.NewCPUSet(1)),
	// 			podToCPUS: map[string]cpuset.CPUSet{
	// 				"1": cpuset.NewCPUSet(1),
	// 			},
	// 		}),
	// 		cgroupConfig: &CgroupConfig{
	// 			Name: cgroupName,
	// 			ResourceParameters: &ResourceConfig{
	// 				CpusetCpus: testCopyString(cpuset.NewCPUSet(1).String()),
	// 			},
	// 		},
	// 		expErrCgroupManager: fmt.Errorf("fake error"),
	// 		expErr:              fmt.Errorf("fake error"),
	// 	},
	// }...)

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			pm := tc.pm
			cccMock := pm.cgroupCPUCFS.(*MockCgroup)
			cccMock.On("ReadPod", tc.pod).
				Return(tc.rcCCC, tc.isTracked).Once()

			// pcmMock := new(MockPodContainerManager)
			// pcmMock.On("GetPodContainerName", tc.pod).
			// 	Return(cgroupName, cgroupPath)
			// ccs.newPodContainerManager = func() PodContainerManager {
			// 	return pcmMock
			// }
			// cmMock := new(MockCgroupManager)
			// // CgroupManager.Update(...) is only called at the last step
			// if tc.expErr == nil {
			// 	cmMock.On("Update", tc.cgroupConfig).
			// 		Return(tc.expErrCgroupManager).
			// 		Once()
			// } else if tc.expErrCgroupManager != nil {
			// 	cmMock.On("Update", tc.cgroupConfig).
			// 		Return(tc.expErrCgroupManager).
			// 		Once()
			// }
			// ccs.cgroupManager = cmMock
			// ccs.activePodsFunc = func() []*v1.Pod {
			// 	return []*v1.Pod{tc.pod}
			// }

			err := pm.updateToHost(tc.pod, tc.isAdded)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			cccMock.AssertExpectations(t)
			// cmMock.AssertExpectations(t)
		})
	}
}
