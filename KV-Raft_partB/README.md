# 基于Raft的键值服务

## PartB：日志压缩
快照是实现日志压缩的最简单的方式。

## 调试
1. `InstallSnapshot RPC`出现`out of range`的错误？
由于我没有完全按照论文中安装快照RPC中的参数，论文中是用`offset`来代表当前日志的偏移量，
而我直接在`LogEntry`结构体中添加一个成员`LogIndex`来代替`offset`。

由于之前的代码没有修改完整，所以导致对日志的操作时出现下标溢出。

在对日志操作的各个地方都要修改，用一个`baseIndex := rf.log[0].LogIndex`来作为每个日志的零点。

2. 卡死在`InstallSnapshot RPC`？
这个错误在lab2扔记忆犹新，因此我特意检查了各个Channel，发现`applyCh`是一个阻塞通道，
果断修改为非阻塞队列。但是依旧卡死。。。

用lab2的测试结果`basic agreement`就已经fail了。

说明修改了lab2的一些代码中出了问题。经检查，发现了一个低级错误：在`Start()`中的
初始化日志没有初始化新添加的`LogIndex`成员，所以在之后`baseIndex`也会使用错误。

修改完之后就能通过PartB的第一个测试`InstallSnapshot RPC`。

3. 通不过测试`persistence with one client and snapshots`
错误说明：
```
2018/11/21 19:03:22 get wrong value, key 0, wanted:
x 0 0 yx 0 1 yx 0 2 yx 0 3 yx 0 4 yx 0 5 yx 0 6 yx 0 7 yx 0 8 yx 0 9 yx 0 10 yx 0 11 yx 0 12 yx 0 13 yx 0 14 yx 0 15 yx 0 16 yx 0 17 yx 0 18 yx 0 19 yx 0 20 yx 0 21 yx 0 22 yx 0 23 yx 0 24 yx 0 25 yx 0 26 y
, got
x 0 0 yx 0 1 yx 0 2 yx 0 3 yx 0 4 yx 0 5 yx 0 6 yx 0 7 yx 0 8 yx 0 9 yx 0 10 yx 0 11 yx 0 12 yx 0 13 yx 0 14 yx 0 15 yx 0 16 yx 0 17 yx 0 18 yx 0 19 yx 0 20 y
exit status 1
```
根据测试给出的提示，是日志缺少了。

找了我一整天，终于发现了bug了。。。而且还是很低级的bug：
在`readSnapshot()`函数中，以下是正确做法：
```go
d.Decode(&LastIncludedIndex)
d.Decode(&LastIncludedTerm)
```
之前我忘了加`&`，导致decode失败，之后的操作必然会受影响。


## 测试
```shell
$ go test
Test: One client ...
  ... Passed
Test: concurrent clients ...
  ... Passed
Test: unreliable ...
  ... Passed
Test: Concurrent Append to same key, unreliable ...
  ... Passed
Test: Progress in majority ...
  ... Passed
Test: No progress in minority ...
  ... Passed
Test: Completion after heal ...
  ... Passed
Test: many partitions ...
2018/11/21 21:24:22 Warning: client 0 managed to perform only 7 put operations in 1 sec?
2018/11/21 21:24:30 Warning: client 0 managed to perform only 7 put operations in 1 sec?
  ... Passed
Test: many partitions, many clients ...
2018/11/21 21:24:45 Warning: client 1 managed to perform only 8 put operations in 1 sec?
  ... Passed
Test: persistence with one client ...
  ... Passed
Test: persistence with concurrent clients ...
  ... Passed
Test: persistence with concurrent clients, unreliable ...
  ... Passed
Test: persistence with concurrent clients and repartitioning servers...
  ... Passed
Test: persistence with concurrent clients and repartitioning servers, unreliable...
2018/11/21 21:26:45 Warning: client 1 managed to perform only 7 put operations in 1 sec?
2018/11/21 21:26:45 Warning: client 2 managed to perform only 5 put operations in 1 sec?
2018/11/21 21:26:45 Warning: client 3 managed to perform only 7 put operations in 1 sec?
2018/11/21 21:26:46 Warning: client 4 managed to perform only 2 put operations in 1 sec?
2018/11/21 21:26:54 Warning: client 0 managed to perform only 4 put operations in 1 sec?
2018/11/21 21:26:55 Warning: client 1 managed to perform only 8 put operations in 1 sec?
2018/11/21 21:26:55 Warning: client 2 managed to perform only 7 put operations in 1 sec?
2018/11/21 21:26:56 Warning: client 3 managed to perform only 8 put operations in 1 sec?
2018/11/21 21:26:56 Warning: client 4 managed to perform only 9 put operations in 1 sec?
2018/11/21 21:27:06 Warning: client 3 managed to perform only 9 put operations in 1 sec?
  ... Passed

Test: InstallSnapshot RPC ...
  ... Passed
Test: persistence with one client and snapshots ...
  ... Passed
Test: persistence with several clients and snapshots ...
  ... Passed
Test: persistence with several clients, snapshots, unreliable ...
  ... Passed
Test: persistence with several clients, failures, and snapshots, unreliable ...
  ... Passed
Test: persistence with several clients, failures, and snapshots, unreliable and partitions ...
2018/11/21 21:28:50 Warning: client 1 managed to perform only 9 put operations in 1 sec?
2018/11/21 21:28:51 Warning: client 3 managed to perform only 8 put operations in 1 sec?
2018/11/21 21:28:51 Warning: client 4 managed to perform only 8 put operations in 1 sec?
2018/11/21 21:29:09 Warning: client 0 managed to perform only 8 put operations in 1 sec?
2018/11/21 21:29:11 Warning: client 4 managed to perform only 6 put operations in 1 sec?
  ... Passed
PASS
ok  	kvraft	351.767s
```
