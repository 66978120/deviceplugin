package deviceplugin

import (
	"context"
	"fmt"
	"log"
	"net"
	"openidl/pkg/common/device"
	"openidl/pkg/common/device/cambricon"
	"os"
	"path"
	"sync"
	"time"

	"google.golang.org/grpc"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type Server struct {
	devs    []*pluginapi.Device
	socket  string
	stop    chan interface{}
	health  chan *pluginapi.Device
	server  *grpc.Server
	options Options
	sync.RWMutex
	device device.Device
}

// NewServer returns an initialized Server
func NewServer(o Options) (*Server, error) {
	device, err := cambricon.NewCambricon()
	if err != nil {
		return nil, err
	}

	s := &Server{
		socket:  ServerSock,
		stop:    make(chan interface{}),
		health:  make(chan *pluginapi.Device),
		options: o,
		device:  device,
	}

	deviceInfos, err := device.GetDeviceInfo()
	if err != nil {
		return nil, err
	}

	for _, info := range deviceInfos {
		s.devs = append(s.devs, &pluginapi.Device{
			ID:     info.Id,
			Health: pluginapi.Healthy,
		})
	}

	return s, nil
}

// dial establishes the gRPC communication with the registered device plugin.
func dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	c, err := grpc.Dial(unixSocketPath, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithTimeout(timeout),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)

	if err != nil {
		return nil, err
	}

	return c, nil
}

// Start starts the gRPC server of the device plugin
func (m *Server) Start() error {
	err := m.cleanup()
	if err != nil {
		return err
	}

	sock, err := net.Listen("unix", m.socket)
	if err != nil {
		return err
	}

	m.server = grpc.NewServer([]grpc.ServerOption{}...)
	pluginapi.RegisterDevicePluginServer(m.server, m)

	go m.server.Serve(sock)

	// Wait for server to start by launching a blocking connection
	conn, err := dial(m.socket, 5*time.Second)
	if err != nil {
		return err
	}
	conn.Close()

	return nil
}

// Stop stops the gRPC server
func (m *Server) Stop() error {
	if m.server == nil {
		return nil
	}

	m.server.Stop()
	m.server = nil
	close(m.stop)

	m.device.Release()
	return m.cleanup()
}

// Register registers the device plugin for the given resourceName with Kubelet.
func (m *Server) Register(kubeletEndpoint, resourceName string) error {
	conn, err := dial(kubeletEndpoint, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(m.socket),
		ResourceName: resourceName,
		Options:      &pluginapi.DevicePluginOptions{},
	}

	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		return err
	}
	return nil
}

func (m *Server) cleanup() error {
	if err := os.Remove(m.socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Serve starts the gRPC server and register the device plugin to Kubelet
func (m *Server) Serve() error {
	if err := m.Start(); err != nil {
		return fmt.Errorf("start device plugin err: %v", err)
	}

	log.Printf("Starting to serve on socket %v", m.socket)
	resourceName := "openi.pcl.ac.cn/cambricon"

	if err := m.Register(pluginapi.KubeletSocket, resourceName); err != nil {
		m.Stop()
		return fmt.Errorf("register resource %s err: %v", resourceName, err)
	}
	log.Printf("Registered resource %s", resourceName)
	return nil
}

func (m *Server) GetDevicePluginOptions(ctx context.Context, empty *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

func (m *Server) ListAndWatch(empty *pluginapi.Empty, server pluginapi.DevicePlugin_ListAndWatchServer) error {
	server.Send(&pluginapi.ListAndWatchResponse{Devices: m.devs})

	for {
		select {
		case <-m.stop:
			return nil
		case d := <-m.health:
			for i, dev := range m.devs {
				if dev.ID == d.ID {
					m.devs[i].Health = d.Health
					break
				}
			}
			server.Send(&pluginapi.ListAndWatchResponse{Devices: m.devs})
		}
	}
}

func (m *Server) GetPreferredAllocation(ctx context.Context, request *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	return &pluginapi.PreferredAllocationResponse{}, nil
}

func (m *Server) Allocate(ctx context.Context, request *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	responses := pluginapi.AllocateResponse{}
	for _, req := range request.ContainerRequests {
		car, err := m.device.GetContainerAllocateResponse(req.DevicesIDs)
		if err != nil {
			return nil, err
		}
		responses.ContainerResponses = append(responses.ContainerResponses, car)
	}
	return &responses, nil
}

func (m *Server) PreStartContainer(ctx context.Context, request *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}
