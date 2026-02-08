package diff;

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareFiles(t *testing.T) {
	// 创建临时测试目录
	tempDir, err := os.MkdirTemp("", "diff_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 创建测试文件
	sourceFile := filepath.Join(tempDir, "source.txt")
	destFile := filepath.Join(tempDir, "dest.txt")

	// 创建源文件
	if err := os.WriteFile(sourceFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// 测试场景1: 目标文件不存在
	different, err := CompareFiles(sourceFile, destFile)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !different {
		t.Errorf("Expected different=true when dest file does not exist, got false")
	}

	// 创建目标文件，内容相同
	if err := os.WriteFile(destFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("Failed to write dest file: %v", err)
	}

	// 测试场景3: 文件相同
	different, err = CompareFiles(sourceFile, destFile)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if different {
		t.Errorf("Expected different=false when files are same, got true")
	}

	// 修改源文件内容
	if err := os.WriteFile(sourceFile, []byte("hello golang"), 0644); err != nil {
		t.Fatalf("Failed to write source file: %v", err)
	}

	// 测试场景4: 文件内容不同
	different, err = CompareFiles(sourceFile, destFile)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !different {
		t.Errorf("Expected different=true when files are different, got false")
	}
}