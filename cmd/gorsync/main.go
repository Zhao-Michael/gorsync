package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	stdsync "sync"

	"gorsync/pkg/net"
	"gorsync/pkg/sync"
)

// #cgo CFLAGS: -I./
// #include <stdlib.h>
import "C"

func main() {
	path := flag.String("path", "", "本地目录路径")
	remote := flag.String("remote", "", "远程地址，格式: host[:port]:path，例如 127.0.0.1:8730:/home/src 或 127.0.0.1:/home/src (默认端口8730)")
	listen := flag.Int("listen", 8730, "启动服务器模式并指定监听端口，默认8730端口(传入0或省略值时使用默认端口)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of gorsync:\n")
		fmt.Fprintf(os.Stderr, "  Sync mode (all operations use TCP, remote-first mode only):\n")
		fmt.Fprintf(os.Stderr, "    gorsync --path <local> --remote <host[:port]:path>")
		fmt.Fprintf(os.Stderr, "  Listen mode:\n")
		fmt.Fprintf(os.Stderr, "    gorsync --listen [<port>]")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	var syncer *sync.Syncer

	var listenFlag bool
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "listen" {
			listenFlag = true
		}
	})

	if flag.NFlag() == 0 {
		listenFlag = true
	}

	if listenFlag {
		port := *listen
		if port == 0 {
			port = 8730
		}
		fmt.Printf("Starting listener on port %d\n", port)

		server := net.NewServer("", port)
		if err := server.Start(); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}

		return
	} else if *remote != "" {
		if *path == "" {
			flag.Usage()
			os.Exit(1)
		}

		absPath, err := filepath.Abs(*path)
		if err != nil {
			log.Fatalf("Invalid path: %v", err)
		}

		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			log.Fatalf("Directory does not exist: %s", absPath)
		}

		host, remotePort, remotePath, err := parseRemoteAddr(*remote)
		if err != nil {
			log.Fatalf("Invalid remote address: %v", err)
		}

		fmt.Printf("Syncing with peer %s:%d\n", host, remotePort)
		fmt.Printf("Local path: %s\n", absPath)
		fmt.Printf("Remote path: %s\n", remotePath)
		fmt.Printf("Sync mode: remote-first\n")
		syncer = sync.NewPeerSyncer(absPath, host, remotePath, remotePort)
	} else {
		flag.Usage()
		os.Exit(1)
	}

	if err := syncer.Sync(); err != nil {
		log.Fatalf("Sync failed: %v", err)
	}

	fmt.Println("Sync completed successfully!")
}

func parseRemoteAddr(remote string) (host string, port int, path string, err error) {
	parts := strings.Split(remote, ":")
	if len(parts) < 2 || len(parts) > 3 {
		err = fmt.Errorf("invalid remote format, expected host[:port]:path")
		return
	}

	if len(parts) == 2 {
		host = parts[0]
		port = 8730
		path = parts[1]
	} else if len(parts) == 3 {
		host = parts[0]
		path = parts[2]
		_, pErr := fmt.Sscanf(parts[1], "%d", &port)
		if pErr != nil {
			port = 8730
		}
	}

	if path == "" {
		err = fmt.Errorf("remote path cannot be empty")
		return
	}

	return
}

// 全局变量，用于存储服务器实例
var (
	serverInstance *net.Server
	serverMutex    = &stdsync.Mutex{}
)

// StartServer 启动服务
//
//export StartServer
func StartServer() C.int {
	// 检查是否已经有服务器实例在运行
	serverMutex.Lock()
	defer serverMutex.Unlock()

	if serverInstance != nil {
		fmt.Printf("Server already running\n")
		return 1 // 失败，服务器已在运行
	}

	// 使用默认的当前目录和 8730 端口
	rootDir := "."
	port := 8730

	// 创建并启动服务器
	serverInstance = net.NewServer(rootDir, port)

	// 在后台启动服务器
	go func() {
		if err := serverInstance.Start(); err != nil {
			fmt.Printf("Failed to start server: %v\n", err)
			// 清理服务器实例
			serverMutex.Lock()
			serverInstance = nil
			serverMutex.Unlock()
		}
	}()
	return 0 // 成功
}

// SyncFiles 同步文件
//
//export SyncFiles
func SyncFiles(localPath *C.char, remotePath *C.char) C.int {
	// 将 C 字符串转换为 Go 字符串
	goLocalPath := C.GoString(localPath)
	goRemotePath := C.GoString(remotePath)
	host, port, path, err := parseRemoteAddr(goRemotePath)

	if err != nil {
		fmt.Printf("Invalid remote address: %v", err)
		return 1 // 失败
	}

	// 创建同步器并执行同步操作
	syncer := sync.NewPeerSyncer(goLocalPath, host, path, port)

	if err := syncer.Sync(); err != nil {
		fmt.Printf("Sync failed: %v\n", err)
		return 1 // 失败
	}

	return 0 // 成功
}

// StopServer 停止所有服务器
//
//export StopServer
func StopServer() C.int {
	// 清理服务器实例
	serverMutex.Lock()
	serverInstance.Stop()
	serverInstance = nil
	serverMutex.Unlock()

	return 0 // 成功
}
