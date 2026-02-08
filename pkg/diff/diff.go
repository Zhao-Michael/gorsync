package diff

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
)

// BlockSize 分块大小，1MB
const BlockSize int64 = 1024 * 1024

// FileBlock 文件块信息
type FileBlock struct {
	Index int64  `json:"index"` // 块索引
	Hash  string `json:"hash"`  // 块的MD5哈希值
	Size  int64  `json:"size"`  // 块大小
}

// CompareFiles 比较两个文件是否不同
func CompareFiles(source, dest string) (bool, error) {
	// 获取源文件信息
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return true, err
	}

	// 获取目标文件信息
	destInfo, err := os.Stat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return true, err
	}

	// 比较文件大小
	if sourceInfo.Size() != destInfo.Size() {
		return true, nil
	}

	// 比较文件类型
	if sourceInfo.IsDir() != destInfo.IsDir() {
		return true, nil
	}

	// 比较文件权限
	if sourceInfo.Mode().Perm() != destInfo.Mode().Perm() {
		return true, nil
	}

	return false, nil
}

// CalculateFileBlocks 计算文件的块信息
func CalculateFileBlocks(filePath string) ([]FileBlock, error) {
	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// 获取文件信息
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %v", err)
	}

	// 计算块数
	numBlocks := (fileInfo.Size() + BlockSize - 1) / BlockSize
	blocks := make([]FileBlock, 0, numBlocks)

	// 缓冲区
	buffer := make([]byte, BlockSize)
	blockIndex := int64(0)

	// 读取文件并计算每个块的哈希值
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read file: %v", err)
		}

		if n == 0 {
			break
		}

		// 计算块的哈希值
		hash := md5.New()
		hash.Write(buffer[:n])
		hashHex := fmt.Sprintf("%x", hash.Sum(nil))

		// 添加块信息
		blocks = append(blocks, FileBlock{
			Index: blockIndex,
			Hash:  hashHex,
			Size:  int64(n),
		})

		blockIndex++
	}

	return blocks, nil
}

// FindDifferentBlocks 查找两个文件块列表中的不同块
func FindDifferentBlocks(sourceBlocks, destBlocks []FileBlock) []int64 {
	// 创建目标块哈希映射
	destBlockMap := make(map[int64]string)
	for _, block := range destBlocks {
		destBlockMap[block.Index] = block.Hash
	}

	// 查找不同的块
	differentBlocks := make([]int64, 0)
	for _, block := range sourceBlocks {
		if destHash, exists := destBlockMap[block.Index]; !exists || destHash != block.Hash {
			differentBlocks = append(differentBlocks, block.Index)
		}
	}

	return differentBlocks
}
