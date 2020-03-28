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

// Package cgroup implement operations for each cgroup value of a pod
package cgroup

import (
	v1 "k8s.io/api/core/v1"
)

// Cgroup interface provides methods for Kubelet to manage pod level cgroup values.
type Cgroup interface {
	// Start is called during PolicyManager initialization.
	Start() error
	// Add a new pod to State, only called by PolicyManager,
	// being idempotent
	AddPod(pod *v1.Pod) error
	// Remove an existing pod from policy manager, only called by PolicyManager
	// being idempotent
	RemovePod(pod *v1.Pod) error
}
