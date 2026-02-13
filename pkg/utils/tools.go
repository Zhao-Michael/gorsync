package utils

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func FormatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

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

// MakeTempName 创建一个临时文件名
func MakeTempName(origname string) string {
	origname = filepath.Clean(origname)

	// 生成10个随机字节
	var rnd [10]byte
	rand.Read(rnd[:]) // 忽略错误

	// 生成临时文件名
	name := "tmp-" + strings.ToLower(base32.StdEncoding.EncodeToString(rnd[:])) + ".tmp"

	return filepath.Join(filepath.Dir(origname), name)
}

// Saferename 安全地重命名文件
func Saferename(oldname, newname string) error {
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
			origtmp = MakeTempName(newname)
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
