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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
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

func TestNewCgroupCPUSet(t *testing.T) {
	_, err := NewCgroupCPUSet()

	assert.Nil(t, err, "Creating cgroupCPUSet failed")
}

func TestCgroupCPUSetStart(t *testing.T) {
	ccc, _ := NewCgroupCPUSet()

	err := ccc.Start()

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
