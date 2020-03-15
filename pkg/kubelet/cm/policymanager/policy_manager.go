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
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"
)

// PolicyManager interface provides methods for Kubelet to manage pod level cgroup values.
type PolicyManager interface {
	Start()
	AddPod(*v1.Pod)
	RemovePod(*v1.Pod)
}

type policyManagerImpl struct {
	// Channel for adding pods
	addPodCh chan *v1.Pod
	// Channel for removing pods
	removePodCh chan *v1.Pod
}

var _ PolicyManager = &policyManagerImpl{}

// NewPolicyManager creates policy manager
func NewPolicyManager() (PolicyManager, error) {
	klog.Infof("[policymanager] Create PolicyManager")
	policyManager := &policyManagerImpl{}
	policyManager.addPodCh = make(chan *v1.Pod)
	policyManager.removePodCh = make(chan *v1.Pod)

	return policyManager, nil
}

// Start is called during Kubelet initialization.
func (p *policyManagerImpl) Start() {
	klog.Infof("[policymanager] Start PolicyManager, %+v", p)
	go p.reconcilePolicyManager()
}

// Add a new pod to policy manager
func (p *policyManagerImpl) AddPod(pod *v1.Pod) {
	klog.Infof("[policymanager] Add pod to PolicyManager, %q", pod.Name)
	p.addPodCh <- pod
}

// Remove an existing pod from policy manager
func (p *policyManagerImpl) RemovePod(pod *v1.Pod) {
	klog.Infof("[policymanager] Remove pod from PolicyManager, %q", pod.Name)
	p.removePodCh <- pod
}

// To prevent race condition,
// policyManagerImpl.reconcilePolicyManager() is the only dedicated go routine to modify the states of PolicyManager
// which takes all requests from kubelet via different channels
func (p *policyManagerImpl) reconcilePolicyManager() {
	for {
		// TODO(liliu) Maybe the order of handling pod adding and removing should be guaranteed.
		// Now the event of removing of the same pod may come before adding the pod.
		// In that case, we can
		// (1) Enforce the order in select statement here, so the removing of the pods should always be handled first.
		// (2) Always check consistency of pods in PolicyManager and PodManager in reconcile.
		select {
		case pod := <-p.addPodCh:
			klog.Infof("[policymanager] Processing addPodCh, %q", pod.Name)
		case pod := <-p.removePodCh:
			klog.Infof("[policymanager] Processing removePodCh, %q", pod.Name)
		}
	}
}
