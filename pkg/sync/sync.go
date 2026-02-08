package sync

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gorsync/pkg/net"
)

// Syncer 同步器结构体
type Syncer struct {
	localPath   string
	remotePath  string
	remoteAddr  string
	port        int
	isListening bool
}

// NewPeerSyncer 创建对等节点模式的同步器
func NewPeerSyncer(localPath, remoteAddr string, remotePath string, port int) *Syncer {
	return &Syncer{
		localPath:   localPath,
		remotePath:  remotePath,
		remoteAddr:  remoteAddr,
		port:        port,
		isListening: true,
	}
}

// Sync 执行同步操作
func (s *Syncer) Sync() error {
	// 打印同步开始信息
	fmt.Printf("Starting sync operation with peer %s:%d\n", s.remoteAddr, s.port)
	fmt.Printf("Local path: %s\n", s.localPath)
	fmt.Printf("Remote path: %s\n", s.remotePath)
	fmt.Printf("Sync mode: remote-first\n")

	// 所有同步操作都通过 TCP 进行
	err := s.syncWithPeer()
	if err == nil {
		fmt.Printf("Sync operation completed successfully with peer %s:%d\n", s.remoteAddr, s.port)
	} else {
		fmt.Printf("Sync operation failed with peer %s:%d: %v\n", s.remoteAddr, s.port, err)
	}
	return err
}

// syncWithPeer 与对等节点同步
func (s *Syncer) syncWithPeer() error {
	// 打印对等节点同步开始信息
	fmt.Printf("Starting peer sync with %s:%d\n", s.remoteAddr, s.port)

	// 启动本地监听服务（仅在监听模式下）
	// 注释掉这部分代码，避免客户端在对等节点模式下启动本地服务器
	// if s.isListening {
	// 	go func() {
	// 		server := net.NewServer(s.localPath, s.port)
	// 		if err := server.Start(); err != nil {
	// 			fmt.Printf("Failed to start local server: %v\n", err)
	// 		}
	// 	}()

	// 	// 等待服务器启动
	// 	fmt.Printf("Started local listener on port %d\n", s.port)
	// }

	// 确保本地目录存在
	if err := os.MkdirAll(s.localPath, 0755); err != nil {
		return fmt.Errorf("failed to create local directory: %v", err)
	}

	client := net.NewClient(s.remoteAddr, s.port)

	// 获取远程文件列表
	// 传递远程路径，让服务器知道要遍历哪个目录
	fmt.Printf("Getting remote files from %s:%d...\n", s.remoteAddr, s.port)
	remoteFiles, err := client.ListFiles(s.remotePath)
	if err != nil {
		return fmt.Errorf("failed to list remote files: %v", err)
	}

	// 打印远程文件列表，用于调试
	fmt.Printf("Remote files: %v\n", remoteFiles)

	// 获取本地文件列表
	fmt.Printf("Getting local files...\n")
	localFiles, err := s.getLocalFiles(s.localPath)
	if err != nil {
		return fmt.Errorf("failed to list local files: %v", err)
	}

	// 执行 remote-first 模式同步
	fmt.Printf("Executing sync in remote-first mode...\n")
	var syncErr error
	syncErr = s.syncRemoteFirst(client, remoteFiles, localFiles)

	if syncErr == nil {
		fmt.Printf("Peer sync completed successfully with %s:%d\n", s.remoteAddr, s.port)
	} else {
		fmt.Printf("Peer sync failed with %s:%d: %v\n", s.remoteAddr, s.port, syncErr)
	}

	return syncErr
}

// syncLocalFirst 本地优先模式同步
func (s *Syncer) syncLocalFirst(client *net.Client, remoteFiles []net.FileInfo, localFiles []net.FileInfo) error {
	// 本地优先模式：本地文件覆盖远程文件
	// 首先上传本地新文件或修改的文件到远程
	for _, localFile := range localFiles {
		if localFile.IsDir {
			continue
		}

		// 检查远程文件是否存在或不同
		remoteFile := s.findFile(remoteFiles, localFile.Path)
		if remoteFile == nil || s.isFileDifferent(localFile, *remoteFile) {
			// 上传文件到远程
			// 注意：这里需要实现上传功能，目前只实现了下载功能
			fmt.Printf("Would upload: %s (local first)\n", localFile.Path)
		}
	}

	// 然后下载远程新文件
	for _, remoteFile := range remoteFiles {
		if remoteFile.IsDir {
			// 创建本地目录
			dirPath := filepath.Join(s.localPath, remoteFile.Path)
			if err := os.MkdirAll(dirPath, os.FileMode(remoteFile.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %v", err)
			}
		} else {
			// 检查本地文件是否存在
			localFile := s.findFile(localFiles, remoteFile.Path)
			if localFile == nil {
				// 下载文件
				localPath := filepath.Join(s.localPath, remoteFile.Path)
				offset := int64(0)
				if info, err := os.Stat(localPath); err == nil {
					offset = info.Size()
				}

				// 构建完整的远程路径
				fullRemotePath := filepath.Join(s.remotePath, remoteFile.Path)
				if err := client.GetFile(fullRemotePath, localPath, offset); err != nil {
					return fmt.Errorf("failed to get file: %v", err)
				}
				fmt.Printf("Downloaded: %s\n", remoteFile.Path)
			}
		}
	}

	return nil
}

// syncRemoteFirst 远程优先模式同步
func (s *Syncer) syncRemoteFirst(client *net.Client, remoteFiles []net.FileInfo, localFiles []net.FileInfo) error {
	// 远程优先模式：远程文件覆盖本地文件
	for _, remoteFile := range remoteFiles {
		if remoteFile.IsDir {
			// 创建本地目录
			dirPath := filepath.Join(s.localPath, remoteFile.Path)
			if err := os.MkdirAll(dirPath, os.FileMode(remoteFile.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %v", err)
			}
		} else {
			// 检查本地文件是否存在或不同
			localFile := s.findFile(localFiles, remoteFile.Path)
			if localFile == nil || s.isFileDifferent(remoteFile, *localFile) {
				// 下载文件
				localPath := filepath.Join(s.localPath, remoteFile.Path)
				offset := int64(0)
				if info, err := os.Stat(localPath); err == nil {
					offset = info.Size()
				}

				// 构建完整的远程路径
				fullRemotePath := filepath.Join(s.remotePath, remoteFile.Path)
				fullRemotePath = strings.ReplaceAll(fullRemotePath, "\\", "/")
				if err := client.GetFile(fullRemotePath, localPath, offset); err != nil {
					return fmt.Errorf("failed to get file: %v", err)
				}
				fmt.Printf("Downloaded: %s\n", remoteFile.Path)
			}
		}
	}

	// 删除本地多余的文件（本地存在但远程不存在的文件）
	for _, localFile := range localFiles {
		// 检查远程文件是否存在
		remoteFile := s.findFile(remoteFiles, strings.ReplaceAll(localFile.Path, "\\", "/"))
		if remoteFile == nil {
			// 远程文件不存在，删除本地文件
			localPath := filepath.Join(s.localPath, localFile.Path)
			_, err := os.Stat(localPath)
			if err == nil {
				if err := os.RemoveAll(localPath); err != nil {
					fmt.Printf("failed to removed: %s\n", localFile.Path)
				}
			}
		}
	}

	return nil
}

// syncBidirectional 双向同步模式
func (s *Syncer) syncBidirectional(client *net.Client, remoteFiles []net.FileInfo, localFiles []net.FileInfo) error {
	// 双向同步模式：较新的文件覆盖较旧的文件

	// 处理远程文件
	for _, remoteFile := range remoteFiles {
		if remoteFile.IsDir {
			// 创建本地目录
			dirPath := filepath.Join(s.localPath, remoteFile.Path)
			if err := os.MkdirAll(dirPath, os.FileMode(remoteFile.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %v", err)
			}
		} else {
			// 检查本地文件是否存在
			localFile := s.findFile(localFiles, remoteFile.Path)
			if localFile == nil {
				// 下载文件
				localPath := filepath.Join(s.localPath, remoteFile.Path)
				offset := int64(0)
				if info, err := os.Stat(localPath); err == nil {
					offset = info.Size()
				}

				// 构建完整的远程路径
				fullRemotePath := filepath.Join(s.remotePath, remoteFile.Path)
				if err := client.GetFile(fullRemotePath, localPath, offset); err != nil {
					return fmt.Errorf("failed to get file: %v", err)
				}
				fmt.Printf("Downloaded: %s\n", remoteFile.Path)
			} else if s.isFileDifferent(remoteFile, *localFile) {
				// 远程文件不同，下载
				localPath := filepath.Join(s.localPath, remoteFile.Path)
				offset := int64(0)
				if info, err := os.Stat(localPath); err == nil {
					offset = info.Size()
				}

				// 构建完整的远程路径
				fullRemotePath := filepath.Join(s.remotePath, remoteFile.Path)
				if err := client.GetFile(fullRemotePath, localPath, offset); err != nil {
					return fmt.Errorf("failed to get file: %v", err)
				}
				fmt.Printf("Downloaded (different): %s\n", remoteFile.Path)
			}
		}
	}

	// 处理本地文件（上传到远程）
	for _, localFile := range localFiles {
		if localFile.IsDir {
			continue
		}

		// 检查远程文件是否存在
		remoteFile := s.findFile(remoteFiles, localFile.Path)
		if remoteFile == nil || s.isFileDifferent(localFile, *remoteFile) {
			// 上传文件到远程
			// 注意：这里需要实现上传功能，目前只实现了下载功能
			fmt.Printf("Would upload (different): %s\n", localFile.Path)
		}
	}

	return nil
}

// isRemoteFileDifferent 检查远程文件是否不同
func (s *Syncer) isRemoteFileDifferent(localPath string, remoteFile *net.FileInfo) (bool, error) {
	// 检查本地文件是否存在
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		return true, nil
	}

	// 获取本地文件信息
	localInfo, err := os.Stat(localPath)
	if err != nil {
		return true, err
	}

	// 比较文件大小
	if localInfo.Size() != remoteFile.Size {
		return true, nil
	}

	// 比较文件类型
	if (localInfo.IsDir() && !remoteFile.IsDir) || (!localInfo.IsDir() && remoteFile.IsDir) {
		return true, nil
	}

	// 比较文件权限
	if int(localInfo.Mode().Perm()) != remoteFile.Mode&0777 {
		return true, nil
	}

	return false, nil
}

// calculateMD5 计算文件的MD5哈希值
func calculateMD5(filePath string) (string, error) {
	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// 创建MD5哈希对象
	hash := md5.New()

	// 读取文件内容并计算哈希值
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to read file: %v", err)
	}

	// 获取哈希值的十六进制表示
	hashHex := fmt.Sprintf("%x", hash.Sum(nil))

	return hashHex, nil
}

// getLocalFiles 获取本地文件列表
func (s *Syncer) getLocalFiles(root string) ([]net.FileInfo, error) {
	var files []net.FileInfo

	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 计算相对路径
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		// 跳过根目录本身
		if relPath == "." {
			return nil
		}

		// 初始化FileInfo
		fileInfo := net.FileInfo{
			Path:    relPath,
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
			IsDir:   info.IsDir(),
			Mode:    int(info.Mode()),
		}

		// 计算文件的MD5哈希值（仅对文件计算，不对目录）
		if !info.IsDir() {
			md5, err := calculateMD5(path)
			if err != nil {
				fmt.Printf("Failed to calculate file MD5 for %s: %v\n", path, err)
				// 继续执行，即使MD5计算失败
			} else {
				fileInfo.MD5 = md5
			}
		}

		files = append(files, fileInfo)

		return nil
	}); err != nil {
		return nil, err
	}

	return files, nil
}

// findFile 在文件列表中查找指定路径的文件
func (s *Syncer) findFile(files []net.FileInfo, path string) *net.FileInfo {
	for i := range files {
		if files[i].Path == path {
			return &files[i]
		}
	}
	return nil
}

// isFileDifferent 检查文件是否不同
func (s *Syncer) isFileDifferent(file1, file2 net.FileInfo) bool {
	// 比较文件类型
	if file1.IsDir != file2.IsDir {
		return true
	}

	// 比较文件权限
	if file1.Mode&0777 != file2.Mode&0777 {
		return true
	}

	// 比较文件大小
	if file1.Size != file2.Size {
		return true
	}

	// 比较MD5值
	if file1.MD5 != "" && file2.MD5 != "" && file1.MD5 != file2.MD5 {
		return true
	}

	return false
}
