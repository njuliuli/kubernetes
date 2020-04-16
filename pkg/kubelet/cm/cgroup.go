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
	v1 "k8s.io/api/core/v1"
)

// Cgroup interface provides methods for Kubelet to manage pod level cgroup values.
type Cgroup interface {
	// Start is called during PolicyManager initialization.
	Start() error
	// AddPod add a new pod to Cgroup,
	// only called by PolicyManager
	// being idempotent
	AddPod(pod *v1.Pod) error
	// RemovePod remove an existing pod from Cgroup,
	// only called by PolicyManager
	// being idempotent
	RemovePod(podUID string) error
	// ReadPod read cgroup values from pod states stored in Cgroup,
	// which is then used to write to host.
	// Even the given pod is not already added (isTracked),
	// or pod == nil (should never happen),
	// a default rc is returned.
	// only called by PolicyManager
	// being idempotent
	ReadPod(pod *v1.Pod) (rc *ResourceConfig, isTracked bool)
}
