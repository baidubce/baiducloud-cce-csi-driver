// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud (interfaces: BOSService)

// Package mock is a generated GoMock package.
package mock

import (
	context "context"
	cloud "github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud"
	gomock "github.com/golang/mock/gomock"
	reflect "reflect"
)

// MockBOSService is a mock of BOSService interface
type MockBOSService struct {
	ctrl     *gomock.Controller
	recorder *MockBOSServiceMockRecorder
}

// MockBOSServiceMockRecorder is the mock recorder for MockBOSService
type MockBOSServiceMockRecorder struct {
	mock *MockBOSService
}

// NewMockBOSService creates a new mock instance
func NewMockBOSService(ctrl *gomock.Controller) *MockBOSService {
	mock := &MockBOSService{ctrl: ctrl}
	mock.recorder = &MockBOSServiceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockBOSService) EXPECT() *MockBOSServiceMockRecorder {
	return m.recorder
}

// BucketExists mocks base method
func (m *MockBOSService) BucketExists(arg0 context.Context, arg1 string, arg2 cloud.Auth) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "BucketExists", arg0, arg1, arg2)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// BucketExists indicates an expected call of BucketExists
func (mr *MockBOSServiceMockRecorder) BucketExists(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "BucketExists", reflect.TypeOf((*MockBOSService)(nil).BucketExists), arg0, arg1, arg2)
}
