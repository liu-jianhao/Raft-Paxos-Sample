# raft_part2
实现Raft算法的第二部分——日志的复制

## 日志复制


## 调试错误
1. 怎么也通不过第三个测试`basic agreement`？
这个错误找了很久，我以为问题是出在日志复制部分，但是怎么也排查不出来。

最后再重新看整个代码，发现`Start()`函数这里没修改，因为这个函数是与’客户‘通信的通道，如果是leader就要添加指令
```go
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	index := -1
	term := rf.currentTerm
	isLeader := rf.state == STATE_LEADER

	if isLeader == true {
		index = rf.getLastIndex() + 1
		rf.log = append(rf.log, LogEntry{LogTerm: term, LogCmd: command})
	}
	return index, term, isLeader
}
```
修改之后就能通过第三个测试了，但是第五个测试没通过。

2. 第五个测试`no agreement if too many followers fail`？
错误提示：
```shell
Test: no agreement if too many followers fail ...
--- FAIL: TestFailNoAgree (3.30s)
	test_test.go:164: 2 committed but no majority
```
既然是跟commint日志相关，那么范围可以缩小到有关日志提交的部分。发现了个低级错误。。。

在`sendAppendEntries`函数中对`matchIndex`的更新出了问题，应该是减一，而不是加一。
```go
rf.nextIndex[server] = reply.NextIndex
rf.matchIndex[server] = reply.NextIndex - 1

```

3. 时不时会卡在第五个测试`no agreement if too many followers fail`？
第五个测试是和领导人选举有关，经检查，在`RequestVote`RPC中，在投票确认的时候没有将重复投票的情况排除。
```go
if uptoDate == true && rf.votedFor == -1 {
	rf.grantedvoteCh <- struct{}{}
	reply.VoteGranted = true
	rf.state = STATE_FLLOWER
	rf.votedFor = args.CandidateId
}
```
在之前的基础上加上了`rf.voteFor == -1`条件。

之后再测试就不会卡在第五个测试点了。


4. 时不时会卡在测试`Figure 8(unreiable)`?
5. 时不时会卡在测试`leader backs up quickly over incorrect follower logs`？
6. 时不时会卡在测试`more persistence`？
找了两天，终于发现问题在哪了。
其实上面的测试卡住不动的原因都是因为我初始化的`Channel`都是无缓冲的，
也就是说从无缓存的频道中读取消息会阻塞，直到有goroutine向该频道中发送消息;
同理，向无缓存的频道中发送消息也会阻塞，直到有goroutine从频道中读取消息。

有缓存的通道类似一个阻塞队列（采用环形数组实现）。当缓存未满时，向通道中发送消息时不会阻塞，
当缓存满时，发送操作将被阻塞，直到有其他goroutine从中读取消息;相应的，当渠道中消息不为空时，
读取消息不会出现阻塞，当渠道为空时，读取操作会造成阻塞，直到有goroutine向渠道中写入消息。

果然，将通道改为有缓存的通道就不会再测试卡住不动了。
```go
rf.heartbeatCh = make(chan interface{}, 100)
rf.leaderCh = make(chan interface{}, 100)
rf.commitCh = make(chan interface{}, 100)
rf.grantedvoteCh = make(chan interface{}, 100)
```

## 测试
```shell
$ go test
Test: initial election ...
  ... Passed
Test: election after network failure ...
  ... Passed
Test: basic agreement ...
  ... Passed
Test: agreement despite follower failure ...
  ... Passed
Test: no agreement if too many followers fail ...
  ... Passed
Test: concurrent Start()s ...
  ... Passed
Test: rejoin of partitioned leader ...
  ... Passed
Test: leader backs up quickly over incorrect follower logs ...
  ... Passed
Test: RPC counts aren't too high ...
  ... Passed
Test: basic persistence ...
  ... Passed
Test: more persistence ...
  ... Passed
Test: partitioned leader and one follower crash, leader restarts ...
  ... Passed
Test: Figure 8 ...
  ... Passed
Test: unreliable agreement ...
  ... Passed
Test: Figure 8 (unreliable) ...
  ... Passed
Test: churn ...
  ... Passed
Test: unreliable churn ...
  ... Passed
PASS
ok  	raft	163.110s

```


