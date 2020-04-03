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
)

func TestNewPolicyManager(t *testing.T) {
	_, err := NewPolicyManager(new(MockCgroupManager),
		fakeNewPodContainerManager)

	assert.Nil(t, err, "Creating PolicyManager failed")
}

func TestPolicyManagerStart(t *testing.T) {
	testCaseArray := []struct {
		description      string
		expErrFromCgroup error
		expErr           error
	}{
		{
			description:      "Success, simple",
			expErrFromCgroup: nil,
			expErr:           nil,
		},
		{
			description:      "Fail, error from Start()",
			expErrFromCgroup: fmt.Errorf("fake error"),
			expErr:           fmt.Errorf("fake error"),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			cgroupMock := new(MockCgroup)
			cgroupMock.On("Start").Return(tc.expErrFromCgroup)
			pm := policyManagerImpl{
				cgroupArray: []Cgroup{cgroupMock},
			}

			err := pm.Start()

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			cgroupMock.AssertExpectations(t)
		})
	}
}

func TestPolicyManagerAddPod(t *testing.T) {
	testCaseArray := []struct {
		description      string
		pod              *v1.Pod
		expErrFromCgroup error
		expErr           error
	}{
		{
			description:      "Success, simple",
			pod:              &v1.Pod{},
			expErrFromCgroup: nil,
			expErr:           nil,
		},
		{
			description:      "Fail, pod not existed",
			pod:              nil,
			expErrFromCgroup: nil,
			expErr:           fmt.Errorf("fake error"),
		},
		{
			description:      "Fail, error from Cgroup.AddPod()",
			pod:              &v1.Pod{},
			expErrFromCgroup: fmt.Errorf("fake error"),
			expErr:           fmt.Errorf("fake error"),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			cgroupMock := new(MockCgroup)
			cgroupMock.On("AddPod", tc.pod).Return(tc.expErrFromCgroup)
			pm := policyManagerImpl{
				cgroupArray: []Cgroup{cgroupMock},
			}

			err := pm.AddPod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			if tc.pod != nil {
				cgroupMock.AssertExpectations(t)
			}
		})
	}
}

func TestPolicyManagerRemovePod(t *testing.T) {
	testCaseArray := []struct {
		description      string
		pod              *v1.Pod
		expErrFromCgroup error
		expErr           error
	}{
		{
			description:      "Success, simple",
			pod:              &v1.Pod{},
			expErrFromCgroup: nil,
			expErr:           nil,
		},
		{
			description:      "Fail, pod not existed",
			pod:              nil,
			expErrFromCgroup: nil,
			expErr:           fmt.Errorf("fake error"),
		},
		{
			description:      "Fail, error from Cgroup.RemovePod()",
			pod:              &v1.Pod{},
			expErrFromCgroup: fmt.Errorf("fake error"),
			expErr:           fmt.Errorf("fake error"),
		},
	}

	for _, tc := range testCaseArray {
		t.Run(tc.description, func(t *testing.T) {
			cgroupMock := new(MockCgroup)
			cgroupMock.On("RemovePod", tc.pod).Return(tc.expErrFromCgroup)
			pm := policyManagerImpl{
				cgroupArray: []Cgroup{cgroupMock},
			}

			err := pm.RemovePod(tc.pod)

			if tc.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
			if tc.pod != nil {
				cgroupMock.AssertExpectations(t)
			}
		})
	}
}
