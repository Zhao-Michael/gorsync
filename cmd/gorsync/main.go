package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	stdsync "sync"

	"gorsync/pkg/net"
	"gorsync/pkg/sync"
)

// #cgo CFLAGS: -I./
// #include <stdlib.h>
import "C"

func main() {
	// 命令行参数解析
	path := flag.String("path", "", "本地目录路径")
	remotePath := flag.String("remote", "", "远程目录路径")
	port := flag.Int("port", 8730, "服务端口")
	peerAddr := flag.String("peer", "", "对等节点IP地址")
	listen := flag.Bool("listen", false, "仅启动服务器模式，不发起连接")

	// 自定义Usage信息
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of gorsync:\n")
		fmt.Fprintf(os.Stderr, "  Sync mode (all operations use TCP, remote-first mode only):\n")
		fmt.Fprintf(os.Stderr, "    gorsync --path <local> --peer <ip> --remote <remote> [--port <port>]")
		fmt.Fprintf(os.Stderr, "  Listen mode:\n")
		fmt.Fprintf(os.Stderr, "    gorsync --listen [--port <port>]")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	var syncer *sync.Syncer

	// 监听模式
	if *listen {
		fmt.Printf("Starting listener on port %d\n", *port)

		// 启动服务器，使用空字符串作为根目录
		server := net.NewServer("", *port)
		if err := server.Start(); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}

		return
	} else if *peerAddr != "" {
		// 对等节点模式（所有同步操作都通过 TCP 进行）
		if *path == "" || *remotePath == "" {
			flag.Usage()
			os.Exit(1)
		}

		// 标准化路径
		absPath, err := filepath.Abs(*path)
		if err != nil {
			log.Fatalf("Invalid path: %v", err)
		}

		// 检查本地目录是否存在
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			log.Fatalf("Directory does not exist: %s", absPath)
		}

		fmt.Printf("Syncing with peer %s:%d\n", *peerAddr, *port)
		fmt.Printf("Local path: %s\n", absPath)
		fmt.Printf("Remote path: %s\n", *remotePath)
		fmt.Printf("Sync mode: remote-first\n")
		syncer = sync.NewPeerSyncer(absPath, *peerAddr, *remotePath, *port)
	} else {
		// 必须指定 --peer 参数
		flag.Usage()
		os.Exit(1)
	}

	// 执行同步
	if err := syncer.Sync(); err != nil {
		log.Fatalf("Sync failed: %v", err)
	}

	fmt.Println("Sync completed successfully!")
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
func SyncFiles(localPath *C.char, remoteAddr *C.char, remotePath *C.char, port C.int) C.int {
	// 将 C 字符串转换为 Go 字符串
	goLocalPath := C.GoString(localPath)
	goRemoteAddr := C.GoString(remoteAddr)
	goRemotePath := C.GoString(remotePath)
	goPort := int(port)

	// 创建同步器并执行同步操作
	syncer := sync.NewPeerSyncer(goLocalPath, goRemoteAddr, goRemotePath, goPort)

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
