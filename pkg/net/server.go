package net

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
)

// FileInfo 文件信息结构体
type FileInfo struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	ModTime   int64  `json:"modTime"`
	IsDir     bool   `json:"isDir"`
	Mode      int    `json:"mode"`
	MD5       string `json:"md5,omitempty"`
	BlockSize int64  `json:"blockSize,omitempty"` // 分块大小
	NumBlocks int64  `json:"numBlocks,omitempty"` // 分块数量
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

// Request 请求结构体
type Request struct {
	Type       string `json:"type"` // "list" or "file"
	Path       string `json:"path"`
	Offset     int64  `json:"offset"`
	BlockIndex int64  `json:"blockIndex,omitempty"` // 块索引
	BlockSize  int64  `json:"blockSize,omitempty"`  // 块大小
}

// Response 响应结构体
type Response struct {
	Status  string     `json:"status"` // "ok" or "error"
	Message string     `json:"message,omitempty"`
	Files   []FileInfo `json:"files,omitempty"`
	File    *FileInfo  `json:"file,omitempty"`
}

// Server TCP服务器结构体
type Server struct {
	rootDir string
	port    int
}

// NewServer 创建新的服务器
func NewServer(rootDir string, port int) *Server {
	return &Server{
		rootDir: rootDir,
		port:    port,
	}
}

// Start 启动服务器
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}
	defer listener.Close()

	fmt.Printf("Server started on port %d\n", s.port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("Failed to accept connection: %v\n", err)
			continue
		}

		go s.handleConnection(conn)
	}
}

// handleConnection 处理客户端连接
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	fmt.Printf("Client connected: %s\n", conn.RemoteAddr())

	// 读取请求
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		s.sendError(conn, fmt.Sprintf("Failed to decode request: %v", err))
		fmt.Printf("Error decoding request: %v\n", err)
		return
	}

	switch req.Type {
	case "list":
		s.handleListRequest(conn, req.Path)
	case "file":
		s.handleFileRequest(conn, req.Path, req.Offset, req.BlockIndex, req.BlockSize)
	default:
		s.sendError(conn, fmt.Sprintf("Unknown request type: %s", req.Type))
		fmt.Printf("Unknown request type: %s\n", req.Type)
	}
}

// handleListRequest 处理文件列表请求
func (s *Server) handleListRequest(conn net.Conn, path string) {
	// 确定完整路径
	var fullPath string
	if s.rootDir == "" {
		fullPath = path
	} else {
		fullPath = filepath.Join(s.rootDir, path)
	}

	// 遍历目录
	var files []FileInfo
	if err := filepath.Walk(fullPath, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 计算相对路径
		var relPath string
		if s.rootDir == "" {
			relPath, err = filepath.Rel(path, walkPath)
		} else {
			relPath, err = filepath.Rel(s.rootDir, walkPath)
		}
		if err != nil {
			return err
		}

		fileInfo := FileInfo{
			Path:    relPath,
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
			IsDir:   info.IsDir(),
			Mode:    int(info.Mode()),
		}

		// 计算文件的MD5哈希值（仅对文件计算，不对目录）
		if !info.IsDir() {
			md5, err := calculateMD5(walkPath)
			if err != nil {
				fmt.Printf("Failed to calculate file MD5 for %s: %v\n", walkPath, err)
				// 继续执行，即使MD5计算失败
			} else {
				fileInfo.MD5 = md5
			}
		}

		files = append(files, fileInfo)

		return nil
	}); err != nil {
		s.sendError(conn, fmt.Sprintf("Failed to walk directory: %v", err))
		return
	}

	// 发送响应
	resp := Response{
		Status: "ok",
		Files:  files,
	}
	if err := json.NewEncoder(conn).Encode(&resp); err != nil {
		fmt.Printf("Failed to send response: %v\n", err)
	}
}

// handleFileRequest 处理文件传输请求
func (s *Server) handleFileRequest(conn net.Conn, path string, offset int64, blockIndex int64, blockSize int64) {
	// 确定完整路径
	var fullPath string
	if s.rootDir == "" {
		fullPath = path
	} else {
		fullPath = filepath.Join(s.rootDir, path)
	}

	// 检查文件是否存在
	info, err := os.Stat(fullPath)
	if err != nil {
		s.sendError(conn, fmt.Sprintf("Failed to stat file: %v", err))
		return
	}

	if info.IsDir() {
		s.sendError(conn, "Path is a directory")
		return
	}

	// 打开文件
	file, err := os.Open(fullPath)
	if err != nil {
		s.sendError(conn, fmt.Sprintf("Failed to open file: %v", err))
		return
	}
	defer file.Close()

	// 计算文件的MD5哈希值
	md5, err := calculateMD5(fullPath)
	if err != nil {
		fmt.Printf("Failed to calculate file MD5: %v\n", err)
		// 继续执行，即使MD5计算失败
	}

	// 计算分块信息
	numBlocks := (info.Size() + BlockSize - 1) / BlockSize

	// 发送文件信息
	fileInfo := &FileInfo{
		Path:      path,
		Size:      info.Size(),
		ModTime:   info.ModTime().Unix(),
		IsDir:     info.IsDir(),
		Mode:      int(info.Mode()),
		MD5:       md5,
		BlockSize: BlockSize,
		NumBlocks: numBlocks,
	}

	resp := Response{
		Status: "ok",
		File:   fileInfo,
	}

	if err := json.NewEncoder(conn).Encode(&resp); err != nil {
		fmt.Printf("Failed to send response: %v\n", err)
		return
	}

	conn.Write([]byte("\n"))

	// 确定传输的偏移量和大小
	transferOffset := offset
	transferSize := info.Size() - offset

	// 如果指定了块索引，计算块的偏移量和大小
	if blockIndex >= 0 {
		transferOffset = blockIndex * BlockSize
		transferSize = BlockSize
		if transferOffset+transferSize > info.Size() {
			transferSize = info.Size() - transferOffset
		}
	}

	// 确保文件指针在正确的位置
	if _, err := file.Seek(transferOffset, io.SeekStart); err != nil {
		fmt.Printf("Failed to seek file: %v\n", err)
		return
	}

	// 打印传输开始信息
	if blockIndex >= 0 {
		fmt.Printf("Starting block transfer: %s (block %d, offset: %d, size: %d bytes)\n", path, blockIndex, transferOffset, transferSize)
	} else {
		fmt.Printf("Starting file transfer: %s (offset: %d, size: %d bytes)\n", path, transferOffset, transferSize)
	}

	// 发送文件数据
	buffer := make([]byte, 64*1024)
	remaining := transferSize
	transferred := int64(0)
	lastProgress := float64(0)

	for remaining > 0 {
		readSize := int64(len(buffer))
		if readSize > remaining {
			readSize = remaining
		}

		n, err := file.Read(buffer[:readSize])
		if err != nil && err != io.EOF {
			fmt.Printf("Failed to read file: %v\n", err)
			return
		}

		if n == 0 {
			break
		}

		if _, err := conn.Write(buffer[:n]); err != nil {
			fmt.Printf("Failed to write to connection: %v\n", err)
			return
		}

		remaining -= int64(n)
		transferred += int64(n)

		// 计算进度并打印
		progress := float64(transferred) / float64(transferSize) * 100
		if progress-lastProgress >= 10 {
			if blockIndex >= 0 {
				fmt.Printf("Block transfer progress: %s (block %d) %.1f%%\n", path, blockIndex, progress)
			} else {
				fmt.Printf("File transfer progress: %s %.1f%%\n", path, progress)
			}
			lastProgress = progress
		}
	}

	// 打印传输完成信息
	if blockIndex >= 0 {
		fmt.Printf("Block transfer completed: %s (block %d, transferred: %d bytes)\n", path, blockIndex, transferred)
	} else {
		fmt.Printf("File transfer completed: %s (transferred: %d bytes)\n", path, transferred)
	}
}

// sendError 发送错误响应
func (s *Server) sendError(conn net.Conn, message string) {
	resp := Response{
		Status:  "error",
		Message: message,
	}
	if err := json.NewEncoder(conn).Encode(&resp); err != nil {
		fmt.Printf("Failed to send error response: %v\n", err)
	}
}
