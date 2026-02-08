package transfer

import (
	"fmt"
	"gorsync/pkg/diff"
	"gorsync/pkg/utils"
	"io"
	"os"
	"sync"
)

// 常量定义
const (
	// BlockSize 分块大小，1MB
	BlockSize int64 = 1024 * 1024
	// MinParallelSize 最小并行传输大小，1MB
	MinParallelSize int64 = 1024 * 1024
)

// copyFileBlock 复制文件的一个块
func copyFileBlock(srcFile, destFile *os.File, offset, size int64) error {
	// 移动文件指针到指定偏移量
	if _, err := srcFile.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek source file: %v", err)
	}

	if _, err := destFile.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek destination file: %v", err)
	}

	// 缓冲区大小
	bufferSize := 64 * 1024
	buffer := make([]byte, bufferSize)

	// 剩余字节数
	remaining := size

	// 复制数据
	for remaining > 0 {
		readSize := bufferSize
		if int64(readSize) > remaining {
			readSize = int(remaining)
		}

		n, err := srcFile.Read(buffer[:readSize])
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read source file: %v", err)
		}

		if n == 0 {
			break
		}

		if _, err := destFile.Write(buffer[:n]); err != nil {
			return fmt.Errorf("failed to write destination file: %v", err)
		}

		remaining -= int64(n)

		// 刷新缓冲区
		if err := destFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync destination file: %v", err)
		}
	}

	return nil
}

// CopyFile 复制文件，支持断点续传、并行传输和增量传输
func CopyFile(source, dest string) error {
	// 计算源文件的MD5哈希值
	srcMD5, err := utils.CalculateMD5(source)
	if err != nil {
		return fmt.Errorf("failed to calculate source file MD5: %v", err)
	}

	// 创建临时文件路径
	tempDest := utils.MakeTempName(dest)

	// 确保函数结束时清理临时文件
	defer func() {
		os.Remove(tempDest)
	}()

	// 打开源文件
	srcFile, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer srcFile.Close()

	// 获取源文件信息
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get source file info: %v", err)
	}

	// 检查目标文件是否存在
	destExists := false
	if _, err := os.Stat(dest); err == nil {
		destExists = true
	}

	// 检查临时文件是否存在
	tempFile, err := os.OpenFile(tempDest, os.O_RDWR|os.O_CREATE, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to open temporary file: %v", err)
	}
	defer tempFile.Close()

	// 获取临时文件信息
	tempInfo, err := tempFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get temporary file info: %v", err)
	}

	// 计算已传输的字节数
	transferred := tempInfo.Size()

	// 如果已传输的字节数大于等于源文件大小，说明文件已经传输完成
	if transferred >= srcInfo.Size() {
		// 计算临时文件的MD5哈希值
		tempMD5, err := utils.CalculateMD5(tempDest)
		if err != nil {
			return fmt.Errorf("failed to calculate temporary file MD5: %v", err)
		}

		// 比较MD5哈希值
		if srcMD5 != tempMD5 {
			return fmt.Errorf("file content mismatch: source MD5 %s, temporary file MD5 %s", srcMD5, tempMD5)
		}

		// 将临时文件重命名为目标文件
		if err := utils.Saferename(tempDest, dest); err != nil {
			return fmt.Errorf("failed to rename temporary file: %v", err)
		}

		fmt.Printf("File already exists and is complete: %s -> %s\n", source, dest)
		return nil
	}

	// 计算剩余需要传输的字节数
	remaining := srcInfo.Size() - transferred

	fmt.Printf("Starting transfer: %s -> %s\n", source, dest)
	fmt.Printf("File size: %.2f MB, Already transferred: %.2f MB, Remaining: %.2f MB\n",
		float64(srcInfo.Size())/1024/1024, float64(transferred)/1024/1024, float64(remaining)/1024/1024)

	// 如果文件大小大于1MB且目标文件存在，使用增量传输
	if srcInfo.Size() > MinParallelSize && destExists {
		fmt.Println("Using delta transfer for large file")
		if err := copyFileDelta(source, tempDest, srcFile, tempFile); err != nil {
			return err
		}
	} else if srcInfo.Size() > MinParallelSize {
		// 如果文件大小大于1MB但目标文件不存在，使用并行传输
		fmt.Println("Using parallel transfer for large file")
		if err := copyFileParallel(srcFile, tempFile, transferred, remaining); err != nil {
			return err
		}
	} else {
		// 否则使用普通传输
		fmt.Println("Using sequential transfer")
		if err := copyFileSequential(srcFile, tempFile, transferred, remaining); err != nil {
			return err
		}
	}

	// 计算临时文件的MD5哈希值
	tempMD5, err := utils.CalculateMD5(tempDest)
	if err != nil {
		return fmt.Errorf("failed to calculate temporary file MD5: %v", err)
	}

	// 比较MD5哈希值
	if srcMD5 != tempMD5 {
		return fmt.Errorf("file content mismatch: source MD5 %s, temporary file MD5 %s", srcMD5, tempMD5)
	}

	// 将临时文件重命名为目标文件
	if err := utils.Saferename(tempDest, dest); err != nil {
		return fmt.Errorf("failed to rename temporary file: %v", err)
	}

	fmt.Printf("File transfer completed successfully: %s -> %s\n", source, dest)
	return nil
}

// copyFileDelta 增量复制文件，只传输不同的块
func copyFileDelta(source, tempDest string, srcFile, tempFile *os.File) error {
	// 计算源文件的块信息
	sourceBlocks, err := diff.CalculateFileBlocks(source)
	if err != nil {
		return fmt.Errorf("failed to calculate source file blocks: %v", err)
	}

	// 计算目标文件的块信息
	destBlocks, err := diff.CalculateFileBlocks(tempDest)
	if err != nil {
		// 如果目标文件不存在或无法计算块信息，使用并行传输
		fmt.Println("Failed to calculate destination file blocks, using parallel transfer instead")
		srcInfo, err := srcFile.Stat()
		if err != nil {
			return fmt.Errorf("failed to get source file info: %v", err)
		}
		return copyFileParallel(srcFile, tempFile, 0, srcInfo.Size())
	}

	// 查找不同的块
	differentBlocks := diff.FindDifferentBlocks(sourceBlocks, destBlocks)

	fmt.Printf("Found %d different blocks out of %d total blocks\n", len(differentBlocks), len(sourceBlocks))

	// 如果所有块都相同，直接返回
	if len(differentBlocks) == 0 {
		fmt.Println("All blocks are identical, no transfer needed")
		return nil
	}

	// 并行传输不同的块
	var wg sync.WaitGroup
	errChan := make(chan error, len(differentBlocks))

	for _, blockIndex := range differentBlocks {
		wg.Add(1)
		go func(index int64) {
			defer wg.Done()

			// 计算块的偏移量
			blockOffset := index * BlockSize

			// 找到对应的块信息
			var blockSize int64
			for _, block := range sourceBlocks {
				if block.Index == index {
					blockSize = block.Size
					break
				}
			}

			fmt.Printf("Transferring block %d (offset: %d, size: %d bytes)\n", index, blockOffset, blockSize)

			// 复制块
			if err := copyFileBlock(srcFile, tempFile, blockOffset, blockSize); err != nil {
				errChan <- fmt.Errorf("failed to copy block %d: %v", index, err)
			} else {
				fmt.Printf("Completed transfer of block %d\n", index)
			}
		}(blockIndex)
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

	fmt.Println("Delta transfer completed")
	return nil
}

// copyFileSequential 顺序复制文件
func copyFileSequential(srcFile, destFile *os.File, offset, size int64) error {
	// 移动源文件指针到指定偏移量
	if _, err := srcFile.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek source file: %v", err)
	}

	// 移动目标文件指针到已传输的位置
	if _, err := destFile.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek destination file: %v", err)
	}

	// 缓冲区大小
	bufferSize := 64 * 1024
	buffer := make([]byte, bufferSize)

	// 开始传输
	transferred := int64(0)
	lastProgress := float64(0)

	for transferred < size {
		n, err := srcFile.Read(buffer)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read source file: %v", err)
		}

		if n == 0 {
			break
		}

		// 写入目标文件
		if _, err := destFile.Write(buffer[:n]); err != nil {
			return fmt.Errorf("failed to write destination file: %v", err)
		}

		// 更新已传输的字节数
		transferred += int64(n)

		// 计算进度并打印
		progress := float64(transferred) / float64(size) * 100
		if progress-lastProgress >= 10 {
			fmt.Printf("Sequential transfer progress: %.1f%%\n", progress)
			lastProgress = progress
		}

		// 刷新缓冲区
		if err := destFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync destination file: %v", err)
		}
	}

	fmt.Println("Sequential transfer completed")
	return nil
}

// copyFileParallel 并行复制文件
func copyFileParallel(srcFile, destFile *os.File, offset, size int64) error {
	// 计算需要的块数
	numBlocks := (size + BlockSize - 1) / BlockSize
	var wg sync.WaitGroup
	errChan := make(chan error, numBlocks)

	fmt.Printf("Starting parallel transfer with %d blocks\n", numBlocks)

	// 启动多个goroutine复制文件块
	for i := int64(0); i < numBlocks; i++ {
		wg.Add(1)
		go func(blockIndex int64) {
			defer wg.Done()

			// 计算当前块的偏移量和大小
			blockOffset := offset + blockIndex*BlockSize
			blockSize := BlockSize
			if blockOffset+blockSize > offset+size {
				blockSize = offset + size - blockOffset
			}

			fmt.Printf("Starting transfer of block %d (offset: %d, size: %d bytes)\n", blockIndex, blockOffset, blockSize)

			// 复制文件块
			if err := copyFileBlock(srcFile, destFile, blockOffset, blockSize); err != nil {
				errChan <- err
			} else {
				fmt.Printf("Completed transfer of block %d\n", blockIndex)
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

	fmt.Println("Parallel transfer completed")
	return nil
}
