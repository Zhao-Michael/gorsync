package net

import (
	"bufio"
	"encoding/json"
	"fmt"
	"gorsync/pkg/utils"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Client TCP客户端结构体
type Client struct {
	addr string
	port int
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

// getFileSequential 顺序获取文件
func (c *Client) DownloadFile(remotePath, localPath string, index int) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	prefix := strings.Repeat(" ", len(strconv.Itoa(index))+2)
	// 发送请求
	req := Request{
		Type:   "file",
		Path:   remotePath,
		Offset: 0,
	}
	if err := json.NewEncoder(conn).Encode(&req); err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}

	reader := bufio.NewReader(conn)
	jsonData, err := reader.ReadBytes('\n')
	ret, err := reader.ReadByte()
	if err != nil || ret != '\n' {
		return fmt.Errorf("failed to parse the \n : %v", err)
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

	// 打印传输开始信息
	fmt.Printf("%d. Starting download (%.2f MB): %s\n", index, float64(resp.File.Size)/1024/1024, remotePath)

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

	// 打开目标文件
	tempFile, err := os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE, os.FileMode(resp.File.Mode))
	if err != nil {
		return fmt.Errorf("failed to open destination file: %v", err)
	}
	defer tempFile.Close()

	// 移动文件指针到指定偏移量
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek destination file: %v", err)
	}

	// 接收文件数据
	buffer := make([]byte, 64*1024)
	transferred := int64(0)
	lastProgress := float64(0)
	totalSize := resp.File.Size

	fmt.Printf("%s>>> Starting download: %s (total size: %d bytes)\n", prefix, remotePath, totalSize)

	for transferred < totalSize {
		n, err := reader.Read(buffer)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read file data: %v", err)
		}

		if n == 0 {
			break
		}

		// 写入目标文件
		if _, err := tempFile.Write(buffer[:n]); err != nil {
			return fmt.Errorf("failed to write destination file: %v", err)
		}

		transferred += int64(n)

		// 计算进度并打印
		progress := float64(transferred) / float64(totalSize) * 100
		if progress-lastProgress >= 10 {
			fmt.Printf("%sSequential download progress: %s %.1f%%\n", prefix, remotePath, progress)
			lastProgress = progress
		}

		// 刷新缓冲区
		if err := tempFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync destination file: %v", err)
		}
	}

	fmt.Printf("%sSequential download completed: %s (transferred: %d bytes)\n", prefix, remotePath, transferred)

	// 确保文件权限正确
	if err := os.Chmod(tempPath, os.FileMode(resp.File.Mode)); err != nil {
		return fmt.Errorf("failed to set destination file mode: %v", err)
	}

	// 计算目标文件的MD5哈希值并与服务器发送的MD5哈希值进行比较
	if resp.File.MD5 != "" {
		destMD5, err := utils.CalculateMD5(tempPath)
		if err != nil {
			return fmt.Errorf("failed to calculate destination file MD5: %v", err)
		}

		if resp.File.MD5 != destMD5 {
			return fmt.Errorf("file content mismatch: server MD5 %s, local MD5 %s", resp.File.MD5, destMD5)
		}

		// 将临时文件重命名为目标文件
		tempFile.Close()
		if err := utils.Saferename(tempPath, localPath); err != nil {
			return fmt.Errorf("failed to rename temporary file: %v", err)
		}

		fmt.Printf("%s<<< Download completed: %s\n", prefix, remotePath)
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
