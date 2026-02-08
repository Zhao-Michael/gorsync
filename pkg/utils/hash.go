package utils

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
)

// CalculateMD5 计算文件的MD5哈希值
func CalculateMD5(filePath string) (string, error) {
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
