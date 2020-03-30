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
		ccsBefore   *cgroupCPUShares
		pod         *v1.Pod
		ccsAfter    *cgroupCPUShares
		expErr      error
	}{
		{
			description: "Success, simple",
			ccsBefore: &cgroupCPUShares{
				podSet: sets.NewString(),
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID: "1",
				},
			},
			expErr: nil,
			ccsAfter: &cgroupCPUShares{
				podSet: sets.NewString("1"),
			},
		},
		{
			description: "Fail, pod not existed",
			ccsBefore: &cgroupCPUShares{
				podSet: sets.NewString(),
			},
			pod:    nil,
			expErr: fmt.Errorf("fake error"),
			ccsAfter: &cgroupCPUShares{
				podSet: sets.NewString(),
			},
		},
		{
			description: "Fail, pod already added",
			ccsBefore: &cgroupCPUShares{
				podSet: sets.NewString("1"),
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID: "1",
				},
			},
			expErr: fmt.Errorf("fake error"),
			ccsAfter: &cgroupCPUShares{
				podSet: sets.NewString("1"),
			},
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

func TestCgroupCPUSharesRemovePod(t *testing.T) {
	testCaseArray := []struct {
		description string
		ccsBefore   *cgroupCPUShares
		pod         *v1.Pod
		ccsAfter    *cgroupCPUShares
		expErr      error
	}{
		{
			description: "Success, simple",
			ccsBefore: &cgroupCPUShares{
				podSet: sets.NewString("1"),
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID: "1",
				},
			},
			expErr: nil,
			ccsAfter: &cgroupCPUShares{
				podSet: sets.NewString(),
			},
		},
		{
			description: "Fail, pod not existed",
			ccsBefore: &cgroupCPUShares{
				podSet: sets.NewString(),
			},
			pod:    nil,
			expErr: fmt.Errorf("fake error"),
			ccsAfter: &cgroupCPUShares{
				podSet: sets.NewString(),
			},
		},
		{
			description: "Fail, pod not added yet",
			ccsBefore: &cgroupCPUShares{
				podSet: sets.NewString(),
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID: "1",
				},
			},
			expErr: fmt.Errorf("fake error"),
			ccsAfter: &cgroupCPUShares{
				podSet: sets.NewString(),
			},
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
