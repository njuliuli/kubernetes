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

type policyManagerImpl struct {
}

var _ PolicyManager = &policyManagerImpl{}

// NewPolicyManager creates PolicyManager for pod level cgroup values
func NewPolicyManager() (PolicyManager, error) {
	klog.Infof("[policymanager] Create policyManagerImpl")

	policyManager := &policyManagerImpl{}

	return policyManager, nil
}

func (p *policyManagerImpl) Start() (rerr error) {
	klog.Infof("[policymanager] Start policyManagerImpl, %+v", p)

	return nil
}

func (p *policyManagerImpl) AddPod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}

	klog.Infof("[policymanager] Add pod to policyManagerImpl, %q", pod.Name)

	return nil
}

func (p *policyManagerImpl) RemovePod(pod *v1.Pod) (rerr error) {
	if pod == nil {
		return fmt.Errorf("pod not exist")
	}

	klog.Infof("[policymanager] Remove pod from policyManagerImpl, %q", pod.Name)

	return nil
}
