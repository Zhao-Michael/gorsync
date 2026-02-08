package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFile(t *testing.T) {
	// 创建临时测试目录
	tempDir, err := os.MkdirTemp("", "transfer_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 创建测试文件
	sourceFile := filepath.Join(tempDir, "source.txt")
	destFile := filepath.Join(tempDir, "dest.txt")

	// 写入测试内容
	testContent := []byte("hello world from gorsync")
	if err := os.WriteFile(sourceFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// 测试完整传输
	if err := CopyFile(sourceFile, destFile); err != nil {
		t.Fatalf("Failed to copy file: %v", err)
	}

	// 验证文件内容
	destContent, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read dest file: %v", err)
	}
	if string(destContent) != string(testContent) {
		t.Errorf("Expected content '%s', got '%s'", string(testContent), string(destContent))
	}

	// 测试断点续传
	// 修改源文件内容
	newContent := []byte("hello world from gorsync with断点续传")
	if err := os.WriteFile(sourceFile, newContent, 0644); err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// 截断目标文件，模拟传输中断
	if err := os.Truncate(destFile, int64(len(testContent)/2)); err != nil {
		t.Fatalf("Failed to truncate dest file: %v", err)
	}

	// 继续传输
	if err := CopyFile(sourceFile, destFile); err != nil {
		t.Fatalf("Failed to copy file with resume: %v", err)
	}

	// 验证文件内容
	destContent, err = os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("Failed to read dest file: %v", err)
	}
	if string(destContent) != string(newContent) {
		t.Errorf("Expected content '%s', got '%s'", string(newContent), string(destContent))
	}
}