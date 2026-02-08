# 测试网络同步功能的PowerShell脚本

# 清理之前的测试目录
Remove-Item -Path test_server_root, test_client_dest -Recurse -Force -ErrorAction SilentlyContinue

# 创建测试目录和文件
New-Item -ItemType Directory -Path test_server_root, test_client_dest -Force
New-Item -ItemType File -Path test_server_root\test1.txt -Value "Hello from server test1" -Force
New-Item -ItemType File -Path test_server_root\test2.txt -Value "Hello from server test2" -Force

# 显示测试文件
Write-Host "=== Test files created ==="
Get-ChildItem -Path test_server_root

# 启动服务器进程
Write-Host "\n=== Starting server ==="
$serverPort = 8730
$serverProcess = Start-Process -FilePath ".\gorsync.exe" -ArgumentList "--listen", "--port", $serverPort -NoNewWindow -PassThru

# 等待服务器启动
Start-Sleep -Seconds 2

# 启动客户端进程进行同步
Write-Host "\n=== Starting client sync ==="
$clientPort = 8731  # 客户端使用不同的端口号
# 获取当前目录的绝对路径
$currentDir = Get-Location
$serverRootPath = Join-Path -Path $currentDir -ChildPath "test_server_root"
# 客户端连接到服务器的8730端口，而自己的本地服务器使用8731端口
$clientProcess = Start-Process -FilePath ".\gorsync.exe" -ArgumentList "--path", "test_client_dest", "--peer", "127.0.0.1", "--remote", $serverRootPath, "--port", $serverPort -NoNewWindow -PassThru -Wait

# 等待客户端完成
Start-Sleep -Seconds 2

# 停止服务器进程
Write-Host "\n=== Stopping server ==="
try {
    $serverProcess.Kill()
    $serverProcess.WaitForExit()
} catch {
    Write-Host "Server process already exited"
}

# 验证同步结果
Write-Host "\n=== Sync result ==="
Write-Host "Server files:"
Get-ChildItem -Path test_server_root

Write-Host "\nClient files:"
Get-ChildItem -Path test_client_dest

# 检查文件内容
Write-Host "\n=== File contents ==="
if (Test-Path "test_client_dest\test1.txt") {
    Write-Host "test1.txt content:"
    Get-Content "test_client_dest\test1.txt"
}

if (Test-Path "test_client_dest\test2.txt") {
    Write-Host "\ntest2.txt content:"
    Get-Content "test_client_dest\test2.txt"
}

# 清理测试目录
Remove-Item -Path test_server_root, test_client_dest -Recurse -Force -ErrorAction SilentlyContinue

Write-Host "\n=== Test completed ==="
