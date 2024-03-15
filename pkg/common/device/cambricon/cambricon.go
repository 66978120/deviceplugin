package cambricon

// #cgo LDFLAGS: -ldl -Wl,--unresolved-symbols=ignore-in-object-files
// #include "cndev.h"
// #include <dlfcn.h>
import "C"
import (
	"fmt"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	"log"
	"openidl/pkg/common/device"
	"path/filepath"
	"unsafe"
)

type Cambricon struct {
	handles []unsafe.Pointer
}

func (c *Cambricon) GetContainerAllocateResponse(ids []string) (*pluginapi.ContainerAllocateResponse, error) {
	r := &pluginapi.ContainerAllocateResponse{}
	if hostDeviceExistsWithPrefix(mluMonitorDeviceName) {
		r.Devices = append(r.Devices, &pluginapi.DeviceSpec{
			HostPath:      mluMonitorDeviceName,
			ContainerPath: mluMonitorDeviceName,
			Permissions:   "rw",
		})
	}

	for idx, id := range ids {
		r.Devices = append(r.Devices, &pluginapi.DeviceSpec{
			HostPath:      fmt.Sprintf("%s%s", mluDeviceNamePrefix, id),
			ContainerPath: fmt.Sprintf("%s%d", mluDeviceNamePrefix, idx),
			Permissions:   "rw",
		})
	}

	r.Mounts = append(r.Mounts, &pluginapi.Mount{
		ContainerPath: cnmonPath,
		HostPath:      cnmonPath,
		ReadOnly:      true,
	})

	return r, nil
}

func NewCambricon() (device.Device, error) {
	handle := C.dlopen(C.CString("libcndev.so"), C.RTLD_LAZY|C.RTLD_GLOBAL)
	if handle == C.NULL {
		return nil, fmt.Errorf("load so failed")
	}
	r := C.cndevInit(C.int(0))
	err := errorString(r)
	if err != nil {
		return nil, err
	}

	c := &Cambricon{}
	c.handles = append(c.handles, handle)
	return c, nil
}

const (
	version              = 5
	mluDeviceNamePrefix  = "/dev/cambricon_dev"
	mluMonitorDeviceName = "/dev/cambricon_ctl"
	cnmonPath            = "/usr/bin/cnmon"
)

func errorString(cRet C.cndevRet_t) error {
	if cRet == C.CNDEV_SUCCESS {
		return nil
	}
	err := C.GoString(C.cndevGetErrorString(cRet))
	return fmt.Errorf("cndev: %v", err)
}

func (c *Cambricon) Release() error {
	ret := C.cndevRelease()
	if ret != C.CNDEV_SUCCESS {
		return errorString(ret)
	}

	for _, handle := range c.handles {
		err := C.dlclose(handle)
		if err != 0 {
			return fmt.Errorf("close handle failed")
		}
	}
	return nil
}

func (c *Cambricon) GetDeviceCount() (uint, error) {
	var cardInfos C.cndevCardInfo_t
	cardInfos.version = C.int(version)
	r := C.cndevGetDeviceCount(&cardInfos)
	return uint(cardInfos.number), errorString(r)
}

func (c *Cambricon) GetDeviceInfo() ([]*device.DeviceInfo, error) {
	count, err := c.GetDeviceCount()
	if err != nil {
		return nil, err
	}

	devs := make([]*device.DeviceInfo, 0, count)
	for i := 0; i < int(count); i++ {
		dev := &device.DeviceInfo{
			Id: fmt.Sprintf("%d", i),
		}
		devs = append(devs, dev)
	}

	return devs, nil
}

func hostDeviceExistsWithPrefix(prefix string) bool {
	matches, err := filepath.Glob(prefix + "*")
	if err != nil {
		log.Printf("failed to know if host device with prefix exists, err: %v \n", err)
		return false
	}
	return len(matches) > 0
}
