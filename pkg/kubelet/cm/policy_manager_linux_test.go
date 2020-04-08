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
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager"
	cputopology "k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
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

// Generate pod with pod.Policy
func testGeneratePodPolicy(policy string) *v1.Pod {
	return testGeneratePod(policy, "", "", "")
}

// Check if the cgroup values in two policyManagerImpl equal
func testEqualPolicyManagerImpl(t *testing.T,
	expect *policyManagerImpl, actual *policyManagerImpl) {
	assert.Equal(t, expect.cgroupCPUCFS, actual.cgroupCPUCFS)
	assert.Equal(t, expect.cgroupCPUSet, actual.cgroupCPUSet)
}

func TestNewPolicyManager(t *testing.T) {
	cpuTopologyFake := &cputopology.CPUTopology{}
	cpusSpecificFake := cpuset.NewCPUSet()
	nodeAllocatableReservationFake := v1.ResourceList{}
	cgroupCPUCFSFake := &cgroupCPUCFS{}
	cgroupCPUSetFake := &cgroupCPUSet{}
	policyManagerFake := &policyManagerImpl{
		cgroupCPUCFS: cgroupCPUCFSFake,
		cgroupCPUSet: cgroupCPUSetFake,
	}

	testCaseArray := []struct {
		description  string
		expErrCPUCFS error
		expErrCPUSet error
		expErr       error
	}{
		{
			description: "Success, simple",
		},
		{
			description:  "Fail, error from expErrCPUCFS.Start()",
			expErrCPUCFS: fmt.Errorf("fake error"),
			expErr:       fmt.Errorf("fake error"),
		},
		{
			description:  "Fail, error from expErrCPUSet.Start()",
			expErrCPUSet: fmt.Errorf("fake error"),
			expErr:       fmt.Errorf("fake error"),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			newCgroupCPUCFS := func(cgroupManager CgroupManager,
				newPodContainerManager typeNewPodContainerManager) (Cgroup, error) {
				if tc.expErrCPUCFS != nil {
					return nil, tc.expErrCPUCFS
				}
				return cgroupCPUCFSFake, nil
			}

			newCgroupCPUSet := func(cpuTopology *cputopology.CPUTopology,
				takeByTopologyFunc cpumanager.TypeTakeByTopologyFunc,
				cpusSpecific cpuset.CPUSet,
				nodeAllocatableReservation v1.ResourceList) (Cgroup, error) {
				if tc.expErrCPUSet != nil {
					return nil, tc.expErrCPUSet
				}
				return cgroupCPUSetFake, nil
			}

			newPolicyManager, err := NewPolicyManager(newCgroupCPUCFS, newCgroupCPUSet,
				new(MockCgroupManager), fakeNewPodContainerManager,
				cpuTopologyFake, cpusSpecificFake, nodeAllocatableReservationFake)

			if tc.expErr == nil {
				assert.Nil(t, err)
				testEqualPolicyManagerImpl(t, policyManagerFake,
					newPolicyManager.(*policyManagerImpl))
			} else {
				assert.Error(t, err)
			}
		})
	}

}

func TestPolicyManagerStart(t *testing.T) {
	testCaseArray := []struct {
		description  string
		expErrCPUCFS error
		expErrCPUSet error
		expErr       error
	}{
		{
			description: "Success, simple",
		},
		{
			description:  "Fail, error from expErrCPUCFS.Start()",
			expErrCPUCFS: fmt.Errorf("fake error"),
			expErr:       fmt.Errorf("fake error"),
		},
		{
			description:  "Fail, error from expErrCPUSet.Start()",
			expErrCPUSet: fmt.Errorf("fake error"),
			expErr:       fmt.Errorf("fake error"),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			cgroupCPUCFSMock := new(MockCgroup)
			cgroupCPUCFSMock.On("Start").Return(tc.expErrCPUCFS)
			cgroupCPUSetMock := new(MockCgroup)
			cgroupCPUSetMock.On("Start").Return(tc.expErrCPUSet)
			pm := policyManagerImpl{
				cgroupCPUCFS: cgroupCPUCFSMock,
				cgroupCPUSet: cgroupCPUSetMock,
			}

			err := pm.Start()

			if tc.expErr == nil {
				assert.Nil(t, err)
				cgroupCPUCFSMock.AssertExpectations(t)
				cgroupCPUSetMock.AssertExpectations(t)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestPolicyManagerAddPod(t *testing.T) {
	testCaseArray := []struct {
		description  string
		pod          *v1.Pod
		expErrCPUCFS error
		expErrCPUSet error
		expErr       error
	}{
		{
			description: "Success, policy=policyCPUCSF",
			pod:         testGeneratePodPolicy(policyCPUCFS),
		},
		{
			description: "Success, policy=policyCPUSet",
			pod:         testGeneratePodPolicy(policyCPUSet),
		},
		{
			description: "Fail, policy unknown",
			pod:         testGeneratePodPolicy(policyUnknown),
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description: "Fail, pod not existed",
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description:  "Fail, policy=policyCPUCSF, error from cgroupCPUCFS.AddPod()",
			pod:          testGeneratePodPolicy(policyCPUCFS),
			expErrCPUCFS: fmt.Errorf("fake error"),
			expErr:       fmt.Errorf("fake error"),
		},
		{
			description:  "Fail, policy=policyCPUSet, error from cgroupCPUSet.AddPod()",
			pod:          testGeneratePodPolicy(policyCPUSet),
			expErrCPUSet: fmt.Errorf("fake error"),
			expErr:       fmt.Errorf("fake error"),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			cgroupCPUCFSMock := new(MockCgroup)
			if tc.pod != nil && tc.pod.Spec.Policy == policyCPUCFS {
				cgroupCPUCFSMock.
					On("AddPod", tc.pod).
					Return(tc.expErrCPUCFS).
					Once()
			}
			cgroupCPUSetMock := new(MockCgroup)
			if tc.pod != nil && tc.pod.Spec.Policy == policyCPUSet {
				cgroupCPUSetMock.
					On("AddPod", tc.pod).
					Return(tc.expErrCPUSet).
					Once()
			}
			pm := policyManagerImpl{
				cgroupCPUCFS: cgroupCPUCFSMock,
				cgroupCPUSet: cgroupCPUSetMock,
			}

			err := pm.AddPod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			if tc.pod != nil {
				switch tc.pod.Spec.Policy {
				case policyCPUCFS:
					cgroupCPUCFSMock.AssertExpectations(t)
				case policyCPUSet:
					cgroupCPUSetMock.AssertExpectations(t)
				}
			}
		})
	}
}

func TestPolicyManagerRemovePod(t *testing.T) {
	testCaseArray := []struct {
		description  string
		pod          *v1.Pod
		expErrCPUCFS error
		expErrCPUSet error
		expErr       error
	}{
		{
			description: "Success, policy=policyCPUCSF",
			pod:         testGeneratePodPolicy(policyCPUCFS),
		},
		{
			description: "Success, policy=policyCPUSet",
			pod:         testGeneratePodPolicy(policyCPUSet),
		},
		{
			description: "Fail, policy unknown",
			pod:         testGeneratePodPolicy(policyUnknown),
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description: "Fail, pod not existed",
			expErr:      fmt.Errorf("fake error"),
		},
		{
			description:  "Fail, policy=policyCPUCSF, error from cgroupCPUCFS.RemovePod()",
			pod:          testGeneratePodPolicy(policyCPUCFS),
			expErrCPUCFS: fmt.Errorf("fake error"),
			expErr:       fmt.Errorf("fake error"),
		},
		{
			description:  "Fail, policy=policyCPUSet, error from cgroupCPUSet.RemovePod()",
			pod:          testGeneratePodPolicy(policyCPUSet),
			expErrCPUSet: fmt.Errorf("fake error"),
			expErr:       fmt.Errorf("fake error"),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			cgroupCPUCFSMock := new(MockCgroup)
			if tc.pod != nil && tc.pod.Spec.Policy == policyCPUCFS {
				cgroupCPUCFSMock.
					On("RemovePod", tc.pod).
					Return(tc.expErrCPUCFS).
					Once()
			}
			cgroupCPUSetMock := new(MockCgroup)
			if tc.pod != nil && tc.pod.Spec.Policy == policyCPUSet {
				cgroupCPUSetMock.
					On("RemovePod", tc.pod).
					Return(tc.expErrCPUSet).
					Once()
			}
			pm := policyManagerImpl{
				cgroupCPUCFS: cgroupCPUCFSMock,
				cgroupCPUSet: cgroupCPUSetMock,
			}

			err := pm.RemovePod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			if tc.pod != nil {
				switch tc.pod.Spec.Policy {
				case policyCPUCFS:
					cgroupCPUCFSMock.AssertExpectations(t)
				case policyCPUSet:
					cgroupCPUSetMock.AssertExpectations(t)
				}
			}
		})
	}
}
