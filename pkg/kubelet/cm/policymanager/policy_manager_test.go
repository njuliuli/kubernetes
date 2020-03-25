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

package policymanager

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
)

func TestNewPolicyManager(t *testing.T) {
	_, err := NewPolicyManager()

	if err != nil {
		t.Errorf("Creating PolicyManager failed")
	}
}

func TestPolicyManagerStart(t *testing.T) {
	policyManager, _ := NewPolicyManager()

	err := policyManager.Start()

	if err != nil {
		t.Errorf("Starting PolicyManager failed")
	}
}

func TestPolicyManagerAddPod(t *testing.T) {
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
			policyManager := policyManagerImpl{}

			err := policyManager.AddPod(testCase.pod)

			// We only check error behavior, but not the error string
			if testCase.expErr == nil && err != nil {
				t.Errorf("Expect error %v, but got %v", testCase.expErr, err)
			}
		})
	}
}

func TestPolicyManagerRemovePod(t *testing.T) {
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
			policyManager := policyManagerImpl{}

			err := policyManager.RemovePod(testCase.pod)

			if testCase.expErr == nil && err != nil {
				t.Errorf("Expect error %v, but got %v", testCase.expErr, err)
			}
		})
	}
}
