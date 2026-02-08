package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gorsync/pkg/net"
	"gorsync/pkg/sync"
)

func main() {
	// 命令行参数解析
	path := flag.String("path", "", "本地目录路径")
	remotePath := flag.String("remote", "", "远程目录路径")
	port := flag.Int("port", 8730, "服务端口")
	peerAddr := flag.String("peer", "", "对等节点IP地址")
	mode := flag.String("mode", "remote-first", "同步模式 (local-first, remote-first, bidirectional)")
	listen := flag.Bool("listen", false, "仅启动服务器模式，不发起连接")

	// 自定义Usage信息
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of gorsync:\n")
		fmt.Fprintf(os.Stderr, "  Sync mode (all operations use TCP):\n")
		fmt.Fprintf(os.Stderr, "    gorsync --path <local> --peer <ip> --remote <remote> [--port <port>] [--mode <mode>]")
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

		// 解析同步模式
		syncMode := sync.SyncMode(*mode)
		if syncMode != sync.LocalFirst && syncMode != sync.RemoteFirst && syncMode != sync.Bidirectional {
			log.Fatalf("Invalid sync mode: %s. Must be one of: local-first, remote-first, bidirectional", *mode)
		}

		fmt.Printf("Syncing with peer %s:%d\n", *peerAddr, *port)
		fmt.Printf("Local path: %s\n", absPath)
		fmt.Printf("Remote path: %s\n", *remotePath)
		fmt.Printf("Sync mode: %s\n", syncMode)
		syncer = sync.NewPeerSyncer(absPath, *peerAddr, *remotePath, *port, syncMode)
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
