package net

import (
	"encoding/json"
	"fmt"
	"gorsync/pkg/utils"
	"io"
	"net"
	"os"
	"path/filepath"
)

// FileInfo 文件信息结构体
type FileInfo struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
	IsDir   bool   `json:"isDir"`
	Mode    int    `json:"mode"`
	MD5     string `json:"md5,omitempty"`
}

// Request 请求结构体
type Request struct {
	Type   string `json:"type"` // "list" or "file"
	Path   string `json:"path"`
	Offset int64  `json:"offset"`
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
	rootDir  string
	port     int
	listener net.Listener
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

	// 保存监听器到结构体中
	s.listener = listener

	fmt.Printf("Server started on port %d\n", s.port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			// 检查是否是因为监听器被关闭导致的错误
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				fmt.Printf("Failed to accept connection: %v\n", err)
				continue
			}
			// 监听器被关闭，退出循环
			fmt.Printf("Server stopped: %v\n", err)
			break
		}

		go s.handleConnection(conn)
	}

	return nil
}

// Stop 停止服务器
func (s *Server) Stop() error {
	if s.listener != nil {
		fmt.Printf("Stopping server on port %d\n", s.port)
		err := s.listener.Close()
		s.listener = nil
		return err
	}
	return nil
}

// handleConnection 处理客户端连接
func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		fmt.Printf("< Client close: %s\n", conn.RemoteAddr())
		conn.Close()
	}()

	fmt.Printf("> Client connected: %s\n", conn.RemoteAddr())

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
		s.handleFileRequest(conn, req.Path)
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
			md5, err := utils.CalculateMD5(walkPath)
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
func (s *Server) handleFileRequest(conn net.Conn, path string) {
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
	md5, err := utils.CalculateMD5(fullPath)
	if err != nil {
		fmt.Printf("Failed to calculate file MD5: %v\n", err)
		// 继续执行，即使MD5计算失败
	}

	// 发送文件信息
	fileInfo := &FileInfo{
		Path:    path,
		Size:    info.Size(),
		ModTime: info.ModTime().Unix(),
		IsDir:   info.IsDir(),
		Mode:    int(info.Mode()),
		MD5:     md5,
	}

	resp := Response{
		Status: "ok",
		File:   fileInfo,
	}

	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		fmt.Printf("Failed to send response: %v\n", err)
		return
	}

	conn.Write([]byte("\n"))

	// 确定传输的偏移量和大小
	transferSize := info.Size()

	// 确保文件指针在正确的位置
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		fmt.Printf("Failed to seek file: %v\n", err)
		return
	}

	// 打印传输开始信息

	fmt.Printf("Starting transfer: %s (size: %d bytes)\n", path, transferSize)

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
			fmt.Printf("File transfer progress: %s %.1f%%\n", path, progress)
			lastProgress = progress
		}
	}

	// 打印传输完成信息
	fmt.Printf("File transfer completed: %s (transferred: %d bytes)\n", path, transferred)
}

// sendError 发送错误响应
func (s *Server) sendError(conn net.Conn, message string) {
	resp := Response{
		Status:  "error",
		Message: message,
	}
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		fmt.Printf("Failed to send error response: %v\n", err)
	}
}
