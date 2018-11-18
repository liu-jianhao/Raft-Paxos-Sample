## labrpc.go简介
1. 它很像Go的RPC系统，但是带有模拟网络(labrpc.go中的`Network`)
    + 这个模拟的网络会延迟请求和回复
    + 这个模拟的网络会丢失请求和回复
    + 这个模拟的网络会重新排序请求和回复
    + 对之后的实验二测试非常有用
2. 说明线程、互斥锁、通道
3. 完整的RPC包完全使用Go语言编写

## labrpc.go中的数据对象
1. reqMsg：请求消息
2. replyMsg：回复消息
3. ClientEnd：客户端点
4. Network：模拟网络
5. Server：服务器（服务的集合）
6. Service：服务