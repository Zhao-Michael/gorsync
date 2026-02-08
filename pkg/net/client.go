package net

import (
	"bufio"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"gorsync/pkg/utils"
	"io"
	"net"
	"os"
	"path/filepath"
)

// 常量定义
const (
	// BlockSize 分块大小，1MB
	BlockSize int64 = 1024 * 1024
)

// Client TCP客户端结构体
type Client struct {
	addr string
	port int
}

// calculateFileMD5 计算文件的MD5哈希值
func calculateFileMD5(filePath string) (string, error) {
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

// NewClient 创建新的客户端
func NewClient(addr string, port int) *Client {
	// 如果端口为0，使用默认端口8730
	if port == 0 {
		port = 8730
	}
	return &Client{
		addr: addr,
		port: port,
	}
}

// ListFiles 获取文件列表
func (c *Client) ListFiles(path string) ([]FileInfo, error) {
	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// 发送请求
	req := Request{
		Type: "list",
		Path: path,
	}
	if err := json.NewEncoder(conn).Encode(&req); err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}

	// 接收响应
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	if resp.Status != "ok" {
		return nil, fmt.Errorf("server error: %s", resp.Message)
	}

	return resp.Files, nil
}

// GetFile 获取文件，根据MD5值比较文件，如果不同就全量传输覆盖
func (c *Client) GetFile(remotePath, localPath string, offset int64) error {
	// 首先获取文件信息
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	// 发送请求获取文件信息
	req := Request{
		Type:   "file",
		Path:   remotePath,
		Offset: 0,
	}
	if err := json.NewEncoder(conn).Encode(&req); err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}

	// 接收响应
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	if resp.Status != "ok" {
		return fmt.Errorf("server error: %s", resp.Message)
	}

	if resp.File == nil {
		return fmt.Errorf("no file info in response")
	}

	// 检查本地文件是否存在且MD5值相同
	if _, err := os.Stat(localPath); err == nil {
		// 本地文件存在，计算其MD5值
		localMD5, err := calculateFileMD5(localPath)
		if err == nil && resp.File.MD5 != "" && resp.File.MD5 == localMD5 {
			// MD5值相同，跳过下载
			fmt.Printf("Skipping download: %s -> %s (MD5 values match)\n", remotePath, localPath)
			return nil
		}
	}

	// 打印传输开始信息
	fmt.Printf("Starting download: %s -> %s\n", remotePath, localPath)
	fmt.Printf("File size: %.2f MB\n", float64(resp.File.Size)/1024/1024)

	// 确保目标目录存在
	destDir := filepath.Dir(localPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// 创建临时文件路径
	tempPath := utils.MakeTempName(localPath)

	// 确保函数结束时清理临时文件
	defer func() {
		os.Remove(tempPath)
	}()

	// 打开临时文件
	tempFile, err := os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE, os.FileMode(resp.File.Mode))
	if err != nil {
		return fmt.Errorf("failed to open temporary file: %v", err)
	}
	tempFile.Close()

	// 始终使用顺序传输
	fmt.Println("Using sequential transfer")
	err = c.getFileSequential(remotePath, tempPath, 0) // 总是从偏移量0开始传输，全量覆盖
	if err != nil {
		return err
	}

	// 计算临时文件的MD5哈希值
	tempMD5, err := calculateFileMD5(tempPath)
	if err != nil {
		return fmt.Errorf("failed to calculate temporary file MD5: %v", err)
	}

	// 比较MD5哈希值
	if resp.File.MD5 != "" && resp.File.MD5 != tempMD5 {
		return fmt.Errorf("file content mismatch: server MD5 %s, local MD5 %s", resp.File.MD5, tempMD5)
	}

	// 将临时文件重命名为目标文件
	if err := utils.Saferename(tempPath, localPath); err != nil {
		return fmt.Errorf("failed to rename temporary file: %v", err)
	}

	fmt.Printf("Download completed: %s -> %s\n", remotePath, localPath)
	return nil
}

// getFileSequential 顺序获取文件
func (c *Client) getFileSequential(remotePath, localPath string, offset int64) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	// 发送请求
	req := Request{
		Type:   "file",
		Path:   remotePath,
		Offset: offset,
	}
	if err := json.NewEncoder(conn).Encode(&req); err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}

	reader := bufio.NewReader(conn)
	jsonData, err := reader.ReadBytes('\n')
	ret, err := reader.ReadByte()
	if err != nil || ret != '\n' {
		return fmt.Errorf("failed to parse the \\n : %v", err)
	}

	// 接收响应
	var resp Response
	if err := json.Unmarshal(jsonData, &resp); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	if resp.Status != "ok" {
		return fmt.Errorf("server error: %s", resp.Message)
	}

	if resp.File == nil {
		return fmt.Errorf("no file info in response")
	}

	// 打开目标文件
	destFile, err := os.OpenFile(localPath, os.O_RDWR|os.O_CREATE, os.FileMode(resp.File.Mode))
	if err != nil {
		return fmt.Errorf("failed to open destination file: %v", err)
	}
	defer destFile.Close()

	// 移动文件指针到指定偏移量
	if _, err := destFile.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek destination file: %v", err)
	}

	// 接收文件数据
	buffer := make([]byte, 64*1024)
	transferred := offset
	lastProgress := float64(0)
	totalSize := resp.File.Size

	fmt.Printf("Starting sequential download: %s (offset: %d, total size: %d bytes)\n", remotePath, offset, totalSize)

	for transferred < totalSize {
		n, err := reader.Read(buffer)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read file data: %v", err)
		}

		if n == 0 {
			break
		}

		// 写入目标文件
		if _, err := destFile.Write(buffer[:n]); err != nil {
			return fmt.Errorf("failed to write destination file: %v", err)
		}

		transferred += int64(n)

		// 计算进度并打印
		progress := float64(transferred) / float64(totalSize) * 100
		if progress-lastProgress >= 10 {
			fmt.Printf("Sequential download progress: %s %.1f%%\n", remotePath, progress)
			lastProgress = progress
		}

		// 刷新缓冲区
		if err := destFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync destination file: %v", err)
		}
	}

	fmt.Printf("Sequential download completed: %s (transferred: %d bytes)\n", remotePath, transferred)

	// 确保文件权限正确
	if err := os.Chmod(localPath, os.FileMode(resp.File.Mode)); err != nil {
		return fmt.Errorf("failed to set destination file mode: %v", err)
	}

	// 计算目标文件的MD5哈希值并与服务器发送的MD5哈希值进行比较
	if resp.File.MD5 != "" {
		destMD5, err := calculateFileMD5(localPath)
		if err != nil {
			return fmt.Errorf("failed to calculate destination file MD5: %v", err)
		}

		if resp.File.MD5 != destMD5 {
			return fmt.Errorf("file content mismatch: server MD5 %s, local MD5 %s", resp.File.MD5, destMD5)
		}
	}

	return nil
}

// connect 连接到服务器
func (c *Client) connect() (net.Conn, error) {
	addr := net.JoinHostPort(c.addr, fmt.Sprintf("%d", c.port))
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}

	return conn, nil
}
