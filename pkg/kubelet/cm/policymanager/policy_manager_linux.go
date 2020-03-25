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

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"
)

// PolicyManager interface provides methods for Kubelet to manage pod level cgroup values.
type PolicyManager interface {
	Start() error
	AddPod(pod *v1.Pod) error
	RemovePod(pod *v1.Pod) error
}

type policyManagerImpl struct {
}

var _ PolicyManager = &policyManagerImpl{}

// NewPolicyManager creates policy manager
func NewPolicyManager() (PolicyManager, error) {
	klog.Infof("[policymanager] Create PolicyManager")

	policyManager := &policyManagerImpl{}

	return policyManager, nil
}

// Start is called during Kubelet initialization.
func (p *policyManagerImpl) Start() (rerr error) {
	klog.Infof("[policymanager] Start PolicyManager, %+v", p)

	return nil
}

// Add a new pod to policy manager,
// as the exported API, it may be called concurrently
func (p *policyManagerImpl) AddPod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("[policymanager] Pod not exist")

	}

	klog.Infof("[policymanager] Add pod to PolicyManager, %q", pod.Name)

	return nil
}

// Remove an existing pod from policy manager
// as the exported API, it may be called concurrently
func (p *policyManagerImpl) RemovePod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("[policymanager] Pod not exist")

	}

	klog.Infof("[policymanager] Remove pod from PolicyManager, %q", pod.Name)

	return nil
}
