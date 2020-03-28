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

package cgroup

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

func TestNewCgroupCPUShares(t *testing.T) {
	_, err := NewCgroupCPUShares()

	assert.Nil(t, err, "Creating cgroupCPUShares failed")
}

func TestCgroupCPUSharesStart(t *testing.T) {
	ccs, _ := NewCgroupCPUShares()

	err := ccs.Start()

	assert.Nil(t, err, "Starting cgroupCPUShares failed")
}

func TestCgroupCPUSharesAddPod(t *testing.T) {
	testCaseArray := []struct {
		description string
		pod         *v1.Pod
		expErr      error
	}{
		{
			description: "Success, simple",
			pod:         &v1.Pod{},
			expErr:      nil,
		},
		{
			description: "Fail, pod not existed",
			pod:         nil,
			expErr:      fmt.Errorf("Pod not exist"),
		},
	}

	for _, testCase := range testCaseArray {
		t.Run(testCase.description, func(t *testing.T) {
			ccs, _ := NewCgroupCPUShares()

			err := ccs.AddPod(testCase.pod)

			if testCase.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestCgroupCPUSharesRemovePod(t *testing.T) {
	testCaseArray := []struct {
		description string
		pod         *v1.Pod
		expErr      error
	}{
		{
			description: "Success, simple",
			pod:         &v1.Pod{},
			expErr:      nil,
		},
		{
			description: "Fail, pod not existed",
			pod:         nil,
			expErr:      fmt.Errorf("Pod not exist"),
		},
	}

	for _, testCase := range testCaseArray {
		t.Run(testCase.description, func(t *testing.T) {
			ccs, _ := NewCgroupCPUShares()

			err := ccs.RemovePod(testCase.pod)

			if testCase.expErr == nil {
				assert.Nil(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
