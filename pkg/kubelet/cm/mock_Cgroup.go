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

// RemovePod provides a mock function with given fields: pod
func (_m *MockCgroup) RemovePod(pod *v1.Pod) error {
	ret := _m.Called(pod)

	var r0 error
	if rf, ok := ret.Get(0).(func(*v1.Pod) error); ok {
		r0 = rf(pod)
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

// UpdatePod provides a mock function with given fields: pod
func (_m *MockCgroup) UpdatePod(pod *v1.Pod) error {
	ret := _m.Called(pod)

	var r0 error
	if rf, ok := ret.Get(0).(func(*v1.Pod) error); ok {
		r0 = rf(pod)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
