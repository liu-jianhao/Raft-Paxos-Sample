# simple-raft
分布式一致性算法——Raft

## raft.go框架
+ Make()	创建一个Raft服务器
+ Start()	利用这个函数找到leader然后发送指令

我们就是在这个基础上实现我们的代码，测试代码会调用上面两个函数。

## Raft算法实现步骤
1. 实现领导人选举
2. 复制日志
