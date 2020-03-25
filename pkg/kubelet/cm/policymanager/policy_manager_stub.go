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

import v1 "k8s.io/api/core/v1"

// PolicyManagerStub is the test stub for PolicyManager
type PolicyManagerStub struct {
}

var _ PolicyManager = &PolicyManagerStub{}

// Start is the test stub for success run
func (m *PolicyManagerStub) Start() (rerr error) {
	return nil
}

// AddPod is the test stub for success run
func (m *PolicyManagerStub) AddPod(pod *v1.Pod) (rerr error) {
	return nil
}

// RemovePod is the test stub for success run
func (m *PolicyManagerStub) RemovePod(pod *v1.Pod) (rerr error) {
	return nil
}
