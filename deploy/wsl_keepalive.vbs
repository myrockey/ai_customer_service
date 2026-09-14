' WSL VM 保活脚本（隐藏窗口）：保持 Ubuntu-22.04 VM 常驻，避免 Win10 空闲 60s 自动关闭导致 Docker 容器全部停止
' 由计划任务 cs_wsl_keepalive 在登录时启动
CreateObject("Wscript.Shell").Run "C:\Windows\System32\wsl.exe -d Ubuntu-22.04 -e sleep infinity", 0, False
