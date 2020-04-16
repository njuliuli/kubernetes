// Code generated by mockery v1.0.0. DO NOT EDIT.

package cm

import (
	mock "github.com/stretchr/testify/mock"
	v1 "k8s.io/api/core/v1"
)

// MockCgroup is an autogenerated mock type for the Cgroup type
type MockCgroup struct {
	mock.Mock
}

// AddPod provides a mock function with given fields: pod
func (_m *MockCgroup) AddPod(pod *v1.Pod) error {
	ret := _m.Called(pod)

	var r0 error
	if rf, ok := ret.Get(0).(func(*v1.Pod) error); ok {
		r0 = rf(pod)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// ReadPod provides a mock function with given fields: pod
func (_m *MockCgroup) ReadPod(pod *v1.Pod) (*ResourceConfig, bool) {
	ret := _m.Called(pod)

	var r0 *ResourceConfig
	if rf, ok := ret.Get(0).(func(*v1.Pod) *ResourceConfig); ok {
		r0 = rf(pod)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*ResourceConfig)
		}
	}

	var r1 bool
	if rf, ok := ret.Get(1).(func(*v1.Pod) bool); ok {
		r1 = rf(pod)
	} else {
		r1 = ret.Get(1).(bool)
	}

	return r0, r1
}

// RemovePod provides a mock function with given fields: podUID
func (_m *MockCgroup) RemovePod(podUID string) error {
	ret := _m.Called(podUID)

	var r0 error
	if rf, ok := ret.Get(0).(func(string) error); ok {
		r0 = rf(podUID)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Start provides a mock function with given fields:
func (_m *MockCgroup) Start() error {
	ret := _m.Called()

	var r0 error
	if rf, ok := ret.Get(0).(func() error); ok {
		r0 = rf()
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
