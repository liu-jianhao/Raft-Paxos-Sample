# lab2 Raft

## 实验2简介
这次实验是实现Raft算法，这是一个副本状态机协议。在此后的实验会在此基础上建立一个具有容错的key/value存储系统的服务

这次是实验我分为两个部分，第一个部分是完成领导人选举，第二个部分是完成日志复制

### 领导人选举
实现之前要仔细啃论文的5.1和5.2节。在这第一部分，我只完成选举这一部分，有关日志的部分都不关心，因此代码比较简单

先了解一下实验的大概框架：

+ `Raft`这个结构体是每一个服务器持有一个的，作为状态机存在
+ 在这个实验中只有两个RPC，一个是请求投票的`ReqestVote`，另一个是修改日志(附带心跳)的`AppendEntries`
+ 每个服务器的功能都一样，只是状态不同而已，状态只能是跟随者、候选人或者领导者的其中一个
+ 每个服务器通过RPC调用`sendXXX`函数来调用其他服务器的`XXX`函数

#### Raft结构
```go
type Raft struct {
	mu        sync.Mutex
	peers     []*labrpc.ClientEnd
	persister *Persister
	me        int // index into peers[]

	termTimer    *time.Timer      // 任期计时器
	state        int              // 当前状态
	voteAcquired int              // 获得的选票
	voteCh       chan interface{} // 投票的通道
	appendCh     chan interface{} // 领导者通知跟随者或候选人添加日志的通道

	currentTerm int // 服务器看到的最近Term(第一次启动的时候为0,后面单调递增)
	votedFor    int // 当前Term收到的投票候选 (如果没有就为null)
}
```
其中有两个通道是用来给goroutine通信用的，当收到vote或者append的消息时，通知主循环

#### 主循环
```go
// 建一个Raft端点。
// peers参数是通往其他Raft端点处于连接状态下的RPC连接。
// me参数是自己在端点数组中的索引。
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here.
	rf.state = STATE_FLLOWER // 一开始都是跟随者
	rf.votedFor = -1
	rf.currentTerm = 0
	rf.voteCh = make(chan interface{})
	rf.appendCh = make(chan interface{})

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// 开始状态机的循环
	go rf.startLoop()

	return rf
}

// 根据论文5.1中的图4实现的状态机循环
func (rf *Raft) startLoop() {
	rf.termTimer = time.NewTimer(randTermDuration())
	for {
		switch rf.state {
		case STATE_FLLOWER:
			select {
			case <-rf.appendCh:
				rf.termTimer.Reset(randTermDuration())
			case <-rf.voteCh:
				rf.termTimer.Reset(randTermDuration())
			case <-rf.termTimer.C: // 计时器超时，变为候选人
				rf.mu.Lock()
				rf.state = STATE_CANDIDATE
				rf.startElection() // 变为候选人后开始选举
				rf.mu.Unlock()
			}

		case STATE_CANDIDATE:
			select {
			case <-rf.appendCh: // 已经有了新的领导者，所以变为跟随者
				rf.state = STATE_FLLOWER
				rf.votedFor = -1
			case <-rf.termTimer.C: // 计时器超时，重新计时
				rf.termTimer.Reset(randTermDuration())
				rf.startElection()
			default: // 获得的票数超过半数，变为领导者
				if rf.voteAcquired > len(rf.peers)/2 {
					rf.state = STATE_LEADER
				}
			}

		case STATE_LEADER:
			rf.broadcastAppendEntries() // 日志兼心跳
			time.Sleep(100 * time.Millisecond)
		}
	}
}
```
Make函数是这个包开始运行的一个函数，其中先做一些初始化，然后进入主循环。

这个主循环就是整个状态机的工作循环，根据论文的描述来实现



#### 开始选举
```go
func (rf *Raft) startElection() {
	rf.currentTerm++
	rf.votedFor = rf.me // 自己投给自己一票
	rf.voteAcquired = 1 // 自己获得一张选票
	rf.termTimer.Reset(randTermDuration())
	rf.broadcastRequestVote()
}
```


#### 广播通知其他节点
```go
// 下面这个函数实现的是候选人并行的向其他服务器节点发送请求投票的RPC（sendRequestVote）
func (rf *Raft) broadcastRequestVote() {
	args := RequestVoteArgs{
		Term:        rf.currentTerm,
		CandidateId: rf.me,
	}

	for i, _ := range rf.peers {
		if i == rf.me {
			continue
		}

		go func(server int) {
			var reply RequestVoteReply
			// 如果是候选人就发送投票请求
			if rf.state == STATE_CANDIDATE && rf.sendRequestVote(server, args, &reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				if reply.VoteGranted == true {
					rf.voteAcquired++
				} else {
					if reply.Term > rf.currentTerm {
						rf.currentTerm = reply.Term
						rf.state = STATE_FLLOWER
						rf.votedFor = -1
					}
				}
			}
		}(i)
	}
}

// 下面这个函数实现的是领导者并行的向其他服务器节点发送请求日志（包括心跳）的RPC（sendAppendEntries）
func (rf *Raft) broadcastAppendEntries() {
	args := AppendEntriesArgs{
		Term:     rf.currentTerm,
		LeaderId: rf.me,
	}

	for i, _ := range rf.peers {
		if i == rf.me {
			continue
		}

		go func(server int) {
			var reply AppendEntriesReply
			// 如果是领导者就发送
			if rf.state == STATE_LEADER && rf.sendAppendEntries(server, args, &reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				if reply.Success == true {

				} else {
					if reply.Term > rf.currentTerm {
						rf.currentTerm = reply.Term
						rf.state = STATE_FLLOWER
						rf.votedFor = -1
					}
				}
			}
		}(i)
	}
}
```

#### 选举与心跳
```go
func (rf *Raft) sendRequestVote(server int, args RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendAppendEntries(server int, args AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}


func (rf *Raft) RequestVote(args RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here.
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
	} else if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = STATE_FLLOWER
		rf.votedFor = args.CandidateId // 投票
		reply.VoteGranted = true
	} else {
		if rf.votedFor == -1 {
			rf.votedFor = args.CandidateId
			reply.VoteGranted = true
		} else {
			reply.VoteGranted = false
		}
	}

	if reply.VoteGranted == true {
		go func() {
			rf.voteCh <- struct{}{}
		}()
	}
}

func (rf *Raft) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term < rf.currentTerm {
		reply.Success = false
		reply.Term = rf.currentTerm
	} else if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = STATE_FLLOWER
		reply.Success = true
	} else {
		reply.Success = true
	}

	go func() {
		rf.appendCh <- struct{}{}
	}()
}
```
前两个就是提供给服务器调用的RPC，下面是对应的被调用的函数

例如候选人通过`sendRequestVote`调用其他节点的`RequestVote`来进行选举

领导者通过`sendAppendEntries`调用其他节点的`AppendEntries`来通知其他节点进行日志复制及心跳通知


#### 随机的计时器
```go
// 获得一个随机的任期时间周期
func randTermDuration() time.Duration {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return time.Millisecond * time.Duration(r.Int63n(100)+400)
}
```

### 第一部分测试
```shell
go test
```
这只能通过其中的两个测试