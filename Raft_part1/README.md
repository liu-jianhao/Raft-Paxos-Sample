# Raft_part1

首先实现Raft算法的第一个部分——选举算法

# 领导人选举
在这一部分，有关日志复制的部分暂时不会被实现，留到第二部分

## 框架
+ `Raft`这个结构体是每一个服务器持有一个的，作为状态机存在
+ 在这个实验中只有两个RPC，一个是请求投票的`ReqestVote`，另一个是修改日志(附带心跳)的`AppendEntries`
+ 每个服务器的功能都一样，只是状态不同而已，状态只能是跟随者、候选人或者领导者的其中一个
+ 每个服务器通过RPC调用`sendXXX`函数来调用其他服务器的`XXX`函数

## 实现步骤
1. 首先根据论文完善Raft结构体中必要的成员
```go
type Raft struct {
	mu        sync.Mutex
	peers     []*labrpc.ClientEnd
	persister *Persister
	me        int // index into peers[]

	// 当前服务器状态
	state int
	// 候选人获得的票数
	voteAcquired int
	// 传播心跳的通道
	heartbeatCh chan interface{}
	// 当候选人赢得了选举就会利用这个通道传送消息
	leaderCh chan interface{}

	/*
	 * 全部服务器上面的可持久化状态:
	 *  currentTerm 	服务器看到的最近Term(第一次启动的时候为0,后面单调递增)
	 *  votedFor     	当前Term收到的投票候选 (如果没有就为null)
	 */
	currentTerm int
	votedFor int
}
```
每个服务器只可能存在一下三个状态之一：
```go
const (
	STATE_FLLOWER = 0
	STATE_CANDIDATE = 1
	STATE_LEADER = 2
)
```
2. 继续根据论文完善在RPC之间传递和返回的参数一些结构体
+ 选举相关
```go
type RequestVoteArgs struct {
	// 候选人的term
	Term int
	// 候选人在节点数组中的下标
	CandidateId int
}

type RequestVoteReply struct {
	// currnetTerm，用来给候选人更新term
	Term int
	// true就说明候选人得到了选票
	VoteGranted bool
}
```

+ 复制日志相关
```go
type AppendEntriesArgs struct {
	// 领导人的term
	Term int
	// 领导人所在的下标
	LeaderId int
}

type AppendEntriesReply struct {
	// currentTerm，用来给领导人更新term
	Term int
	// true，说明和跟随者的日志匹配
	Success bool
}
```

3. 这一部分的重头戏，实现Make()方法
```go
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// 初始化
	rf.state = STATE_FLLOWER
	rf.votedFor = -1
	rf.currentTerm = 0
	rf.heartbeatCh = make(chan interface{})
	rf.leaderCh = make(chan interface{})

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// 状态机循环
	go func() {
		for {
			switch rf.state {
			case STATE_FLLOWER:
				select {
				// 接收到心跳或者超时就变为候选者
				case <- rf.heartbeatCh:
				case <- time.After(time.Duration(rand.Int63() % 333 + 550) * time.Millisecond):
					rf.state = STATE_CANDIDATE
				}	

			case STATE_LEADER:
				// 如果是领导人就执行复制日志(携带心跳)的操作
				rf.broadcastAppendEntries()
				time.Sleep(50 * time.Millisecond)

			case STATE_CANDIDATE:
				// 候选人要准备投票选举
				rf.mu.Lock()
				rf.currentTerm++
				// 先投给自己一票
				rf.votedFor = rf.me
				rf.voteAcquired = 1
				rf.mu.Unlock()
				go rf.broadcastRequestVote()
				select {
				// 超时或者接收到心跳(其他节点先成为领导人)就变为跟随者
				case <- time.After(time.Duration(rand.Int63() % 333 + 550) * time.Millisecond):
				case <- rf.heartbeatCh:
					rf.state = STATE_FLLOWER	
				// 从leaderCh通道接收到消息说明赢得选举，成为领导人
				case <- rf.leaderCh:
					rf.state = STATE_LEADER
				}	
			}
		}
	}()

	return rf
}
```

4. 实现在第3步中使用却没实现的两个操作：领导人选举和日志复制
+ 领导人选举

下面这个是RPC函数，用来给候选人调用的，然后在每个服务器执行
```go
func (rf *Raft) RequestVote(args RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here.
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm

	// 如果候选人的term比自己的还小，则不给该候选人投票
	if args.Term < rf.currentTerm {
		reply.VoteGranted = false
		return
	} else if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = STATE_FLLOWER
		reply.VoteGranted = true
	}
}
```

```go
func (rf *Raft) sendRequestVote(server int, args RequestVoteArgs, reply *RequestVoteReply) bool {
	// 调用RPC
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if ok {
		term := rf.currentTerm
		if rf.state != STATE_CANDIDATE {
			return ok
		} else if args.Term != term {
			return ok
		}

		// 更新自己的term
		if reply.Term > term {
			rf.currentTerm = reply.Term
			rf.state = STATE_FLLOWER
			rf.votedFor = -1
		}

		// 统计选票
		if reply.VoteGranted == true {
			rf.voteAcquired++
			if rf.voteAcquired > len(rf.peers)/2 {
				rf.state = STATE_FLLOWER
				rf.leaderCh <- true
			}
		}
	}

	return ok
}

func (rf *Raft) broadcastRequestVote() {
	// 遍历整个Raft服务器数组，执行领导人选举
	for i := range rf.peers {
		if i != rf.me {	
			// 初始化要传给RPC调用的函数参数
			var args RequestVoteArgs
			args.Term = rf.currentTerm
			args.CandidateId = rf.me

			go func(i int) {
				var reply RequestVoteReply
				rf.sendRequestVote(i, args, &reply)
			}(i)
		}
	}
}
```

+ 复制日志(在这一部分只实现简单的心跳)

下面这个是RPC函数，用来给候领导人调用的，然后在每个服务器执行
```go
func (rf *Raft) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	rf.heartbeatCh <- struct{}{}
}
```

有了之前实现的投票，类似地实现日志复制
```go
func (rf *Raft) sendAppendEntries(server int, args AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if ok {
		if rf.state != STATE_LEADER {
			return ok
		} else if args.Term != rf.currentTerm {
			return ok
		}

		// 更新自己的term
		if reply.Term > rf.currentTerm {
			rf.currentTerm = reply.Term
			rf.state = STATE_FLLOWER
			rf.votedFor = -1
			return ok
		}
	}

	return ok
}

func (rf *Raft) broadcastAppendEntries() {
	for i := range rf.peers {
		if i != rf.me && rf.state == STATE_LEADER {
			// 参数初始化
			var args AppendEntriesArgs
			args.Term = rf.currentTerm
			args.LeaderId = rf.me

			go func(i int, args AppendEntriesArgs) {
				var reply AppendEntriesReply
				rf.sendAppendEntries(i, args, &reply)
			}(i, args)
		}
	}
}
```

## 测试
只能通过前两个测试点
```shell
$ go test
go test
Test: initial election ...
  ... Passed
Test: election after network failure ...
  ... Passed
Test: basic agreement ...
^Csignal: interrupt
FAIL	raft	12.131s
```
