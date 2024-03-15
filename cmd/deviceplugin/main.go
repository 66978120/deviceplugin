package main

import (
	"log"
	"openidl/pkg/deviceplugin"
	"os"
	"os/signal"
	"syscall"

	"github.com/fsnotify/fsnotify"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

func main() {

	options := deviceplugin.ParseFlags()

	log.Println("Starting FS watcher.")
	watcher, err := startFSWatcher(pluginapi.DevicePluginPath)
	if err != nil {
		log.Printf("Failed to created FS watcher. err: %v", err)
		os.Exit(1)
	}
	defer watcher.Close()

	log.Println("Starting OS watcher.")
	sigs := startOSWatcher(syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	var devicePlugin *deviceplugin.Server

restart:
	if devicePlugin != nil {
		devicePlugin.Stop()
	}
	startErr := make(chan struct{})
	devicePlugin, err = deviceplugin.NewServer(options)
	if err != nil {
		panic("Failed to create device plugin: " + err.Error())
	}
	if err := devicePlugin.Serve(); err != nil {
		log.Printf("serve device plugin err: %v, restarting.", err)
		close(startErr)
		goto events
	}

events:
	for {
		select {
		case <-startErr:
			goto restart
		case event := <-watcher.Events:
			if event.Name == pluginapi.KubeletSocket && event.Op&fsnotify.Create == fsnotify.Create {
				log.Printf("inotify: %s created, restarting.", pluginapi.KubeletSocket)
				goto restart
			}
		case err := <-watcher.Errors:
			log.Printf("inotify err: %v", err)
		case s := <-sigs:
			switch s {
			case syscall.SIGHUP:
				log.Println("Received SIGHUP, restarting.")
				goto restart
			default:
				log.Printf("Received signal %v, shutting down.", s)
				devicePlugin.Stop()
				break events
			}
		}
	}
}

func startFSWatcher(files ...string) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		err = watcher.Add(f)
		if err != nil {
			watcher.Close()
			return nil, err
		}
	}

	return watcher, nil
}

func startOSWatcher(sigs ...os.Signal) chan os.Signal {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, sigs...)

	return sigChan
}
