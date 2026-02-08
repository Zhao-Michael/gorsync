package net

import (
	"bufio"
	"crypto/md5"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 常量定义
const (
	// BlockSize 分块大小，1MB
	BlockSize int64 = 1024 * 1024
	// MinParallelSize 最小并行传输大小，1MB
	MinParallelSize int64 = 1024 * 1024
)

// makeTempName 创建一个临时文件名
func makeTempName(origname, prefix string) (tempname string, err error) {
	origname = filepath.Clean(origname)
	if len(origname) == 0 || origname[len(origname)-1] == filepath.Separator {
		return "", os.ErrInvalid
	}
	// Generate 10 random bytes.
	// This gives 80 bits of entropy, good enough
	// for making temporary file name unpredictable.
	var rnd [10]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	name := prefix + "-" + strings.ToLower(base32.StdEncoding.EncodeToString(rnd[:])) + ".tmp"
	return filepath.Join(filepath.Dir(origname), name), nil
}

// saferename 安全地重命名文件
func saferename(oldname, newname string) error {
	err := os.Rename(oldname, newname)
	if err != nil {
		// If newname exists ("original"), we will try renaming it to a
		// new temporary name, then renaming oldname to the newname,
		// and deleting the renamed original. If system crashes between
		// renaming and deleting, the original file will still be available
		// under the temporary name, so users can manually recover data.
		// (No automatic recovery is possible because after crash the
		// temporary name is not known.)
		var origtmp string
		for {
			origtmp, err = makeTempName(newname, filepath.Base(newname))
			if err != nil {
				return err
			}
			_, err = os.Stat(origtmp)
			if err == nil {
				continue // most likely will never happen
			}
			break
		}
		err = os.Rename(newname, origtmp)
		if err != nil {
			return err
		}
		err = os.Rename(oldname, newname)
		if err != nil {
			// Rename still fails, try to revert original rename,
			// ignoring errors.
			os.Rename(origtmp, newname)
			return err
		}
		// Rename succeeded, now delete original file.
		os.Remove(origtmp)
	}
	return nil
}

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

	// 打印传输开始信息
	fmt.Printf("Starting download: %s -> %s\n", remotePath, localPath)
	fmt.Printf("File size: %.2f MB\n", float64(resp.File.Size)/1024/1024)

	// 确保目标目录存在
	destDir := filepath.Dir(localPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// 创建临时文件路径
	tempPath := localPath + ".tmp"

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

	// 根据文件大小选择传输方式
	if resp.File.Size > MinParallelSize {
		// 使用并行传输
		fmt.Println("Using parallel transfer")
		err = c.getFileParallel(remotePath, tempPath, resp.File)
	} else {
		// 使用顺序传输
		fmt.Println("Using sequential transfer")
		err = c.getFileSequential(remotePath, tempPath, 0) // 总是从偏移量0开始传输，全量覆盖
	}
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
	if err := saferename(tempPath, localPath); err != nil {
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

// getFileParallel 并行获取文件
func (c *Client) getFileParallel(remotePath, localPath string, fileInfo *FileInfo) error {
	// 先创建一个与源文件大小相同的空文件
	destFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %v", err)
	}

	// 设置文件大小
	if err := destFile.Truncate(fileInfo.Size); err != nil {
		destFile.Close()
		return fmt.Errorf("failed to truncate destination file: %v", err)
	}
	destFile.Close()

	// 计算需要的块数
	numBlocks := fileInfo.NumBlocks
	if numBlocks <= 0 {
		numBlocks = (fileInfo.Size + BlockSize - 1) / BlockSize
	}

	fmt.Printf("Starting parallel download: %s (total blocks: %d)\n", remotePath, numBlocks)

	var wg sync.WaitGroup
	errChan := make(chan error, numBlocks)

	// 启动多个goroutine获取文件块
	for i := int64(0); i < numBlocks; i++ {
		wg.Add(1)
		go func(blockIndex int64) {
			defer wg.Done()

			// 获取文件块
			if err := c.GetFileBlock(remotePath, localPath, blockIndex); err != nil {
				errChan <- err
			}
		}(i)
	}

	// 等待所有goroutine完成
	wg.Wait()
	close(errChan)

	// 检查是否有错误
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	fmt.Printf("Parallel download completed: %s (all %d blocks transferred)\n", remotePath, numBlocks)

	// 确保文件权限正确
	if err := os.Chmod(localPath, os.FileMode(fileInfo.Mode)); err != nil {
		return fmt.Errorf("failed to set destination file mode: %v", err)
	}

	// 计算目标文件的MD5哈希值并与服务器发送的MD5哈希值进行比较
	if fileInfo.MD5 != "" {
		destMD5, err := calculateFileMD5(localPath)
		if err != nil {
			return fmt.Errorf("failed to calculate destination file MD5: %v", err)
		}

		if fileInfo.MD5 != destMD5 {
			return fmt.Errorf("file content mismatch: server MD5 %s, local MD5 %s", fileInfo.MD5, destMD5)
		}
	}

	return nil
}

// GetFileBlock 获取文件块
func (c *Client) GetFileBlock(remotePath, localPath string, blockIndex int64) error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	// 发送请求
	req := Request{
		Type:       "file",
		Path:       remotePath,
		BlockIndex: blockIndex,
		BlockSize:  BlockSize,
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

	// 打开目标文件
	destFile, err := os.OpenFile(localPath, os.O_RDWR|os.O_CREATE, os.FileMode(resp.File.Mode))
	if err != nil {
		return fmt.Errorf("failed to open destination file: %v", err)
	}
	defer destFile.Close()

	// 计算块的偏移量
	offset := blockIndex * BlockSize

	// 移动文件指针到指定偏移量
	if _, err := destFile.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek destination file: %v", err)
	}

	// 接收文件数据
	buffer := make([]byte, 64*1024)
	transferred := int64(0)
	lastProgress := float64(0)
	blockSize := BlockSize
	if offset+blockSize > resp.File.Size {
		blockSize = resp.File.Size - offset
	}
	totalSize := blockSize

	for transferred < totalSize {
		n, err := conn.Read(buffer)
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
			fmt.Printf("Block download progress: %s (block %d) %.1f%%\n", remotePath, blockIndex, progress)
			lastProgress = progress
		}

		// 刷新缓冲区
		if err := destFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync destination file: %v", err)
		}
	}

	return nil
}

// connect 连接到服务器
func (c *Client) connect() (net.Conn, error) {
	addr := fmt.Sprintf("%s:%d", c.addr, c.port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %v", err)
	}

	return conn, nil
}
