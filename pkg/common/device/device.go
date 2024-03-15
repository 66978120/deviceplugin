package device

import pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

type DeviceInfo struct {
	Id string
}

type Device interface {
	Release() error
	GetDeviceCount() (uint, error)
	GetDeviceInfo() ([]*DeviceInfo, error)
	GetContainerAllocateResponse(ids []string) (*pluginapi.ContainerAllocateResponse, error)
}
