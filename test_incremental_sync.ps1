# 测试增量同步功能的PowerShell脚本

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

# 第一次同步：应该下载所有文件
Write-Host "\n=== First sync (should download all files) ==="
$clientProcess = Start-Process -FilePath ".\gorsync.exe" -ArgumentList "--path", "test_client_dest", "--peer", "127.0.0.1", "--remote", (Join-Path -Path (Get-Location) -ChildPath "test_server_root"), "--port", $serverPort -NoNewWindow -PassThru -Wait

# 等待客户端完成
Start-Sleep -Seconds 2

# 显示第一次同步后的文件
Write-Host "\n=== Files after first sync ==="
Get-ChildItem -Path test_client_dest

# 第二次同步：应该跳过所有文件，因为它们已经存在且MD5值相同
Write-Host "\n=== Second sync (should skip all files) ==="
$clientProcess = Start-Process -FilePath ".\gorsync.exe" -ArgumentList "--path", "test_client_dest", "--peer", "127.0.0.1", "--remote", (Join-Path -Path (Get-Location) -ChildPath "test_server_root"), "--port", $serverPort -NoNewWindow -PassThru -Wait

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

# 清理测试目录
Remove-Item -Path test_server_root, test_client_dest -Recurse -Force -ErrorAction SilentlyContinue

Write-Host "\n=== Test completed ==="
