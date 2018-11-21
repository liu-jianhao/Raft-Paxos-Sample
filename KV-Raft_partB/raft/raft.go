package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	// "fmt"
	"sync"
	"labrpc"
	"bytes"
	"encoding/gob"
	"time"
	"math/rand"
)

const (
	STATE_LEADER = 0
	STATE_CANDIDATE = 1
	STATE_FLLOWER = 2
)

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make().
//
type ApplyMsg struct {
	Index       int
	Command     interface{}
	UseSnapshot bool   // ignore for lab2; only used in lab3
	Snapshot    []byte // ignore for lab2; only used in lab3
}

// 日志项结构体
type LogEntry struct {
	LogTerm int
	LogComd interface{}
}

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex
	peers     []*labrpc.ClientEnd
	persister *Persister
	me        int // index into peers[]

	// Your data here.
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	// 当前服务器状态
	state int
	// 候选人获得的票数
	voteAcquired int
	// 传播心跳的通道
	heartbeatCh chan interface{}
	// 当候选人赢得了选举就会利用这个通道传送消息
	leaderCh chan interface{}
	// 日志需要提交时传递消息
	commitCh chan interface{}
	// 获得选票时传递消息
	grantedvoteCh chan interface{}

	/*
	 * 全部服务器上面的可持久化状态:
	 *  currentTerm 	服务器看到的最近Term(第一次启动的时候为0,后面单调递增)
	 *  votedFor     	当前Term收到的投票候选 (如果没有就为null)
	 *  log[]        	日志项; 每个日志项包含机器状态和被leader接收的Term(first index is 1)
	 */
	currentTerm int
	votedFor int
	log []LogEntry
		
	/*
	 * 全部服务器上面的不稳定状态:
	 *	commitIndex 	已经被提交的最新的日志索引(第一次为0,后面单调递增)
	 *	lastApplied      已经应用到服务器状态的最新的日志索引(第一次为0,后面单调递增)
	*/
	commitIndex int
	lastApplied int

	/*
	 * leader上面使用的不稳定状态（完成选举之后需要重新初始化）
	 *	nextIndex[] 	每个服务器下一个日志要写入的index	
	 *	matchIndex[] 	每个服务器已经匹配的日志的index	
	*/
	nextIndex []int
	matchIndex []int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	var term int
	var isleader bool
	// Your code here.
	term = rf.currentTerm
	isleader = rf.state == STATE_LEADER

	return term, isleader
}

func (rf *Raft) getLastIndex() int {
	return len(rf.log) - 1
}
func (rf *Raft) getLastTerm() int {
	return rf.log[len(rf.log) - 1].LogTerm
}
//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here.
	// Example:
	// w := new(bytes.Buffer)
	// e := gob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
	w := new(bytes.Buffer)
 	e := gob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	// Your code here.
	// Example:
	// r := bytes.NewBuffer(data)
	// d := gob.NewDecoder(r)
	// d.Decode(&rf.xxx)
	// d.Decode(&rf.yyy)
	r := bytes.NewBuffer(data)
	d := gob.NewDecoder(r)
	d.Decode(&rf.currentTerm)
	d.Decode(&rf.votedFor)
	d.Decode(&rf.log)
}

//
// example RequestVote RPC arguments structure.
//
type RequestVoteArgs struct {
	// Your data here.
	// 候选人的term
	Term int
	// 候选人在节点数组中的下标
	CandidateId int
	// 候选人最后一个日志项的下标
	LastLogIndex int
	// 候选人最后一个日志项的term
	LastLogTerm int
}

//
// example RequestVote RPC reply structure.
//
type RequestVoteReply struct {
	// Your data here.
	// currnetTerm，用来给候选人更新term
	Term int
	// true就说明候选人得到了选票
	VoteGranted bool
}

type AppendEntriesArgs struct {
	// 领导人的term
	Term int
	// 领导人所在的下标
	LeaderId int
	// 准备写入的前一个日志项的term
	PrevLogTerm int
	// 准备写入的前一个日志项的index
	PrevLogIndex int
	// 领导人传来的日志
	Entries []LogEntry
	// 领导人已经提交的最大index
	LeaderCommit int
}

type AppendEntriesReply struct {
	// currentTerm，用来给领导人更新term
	Term int
	// true，说明和跟随者的日志匹配
	Success bool
	// 下一个准备填入日志项的下标
	NextIndex int
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here.
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()

	reply.VoteGranted = false
	// 如果候选人的term比自己的还小，则不给该候选人投票
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		return
	}

	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = STATE_FLLOWER
		rf.votedFor = -1
	}
	reply.Term = rf.currentTerm

	term := rf.getLastTerm()
	index := rf.getLastIndex()

	uptoDate := false
	if args.LastLogTerm > term {
		uptoDate = true
	}

	if args.LastLogTerm == term && args.LastLogIndex >= index { // at least up to date
		uptoDate = true
	}

	// 还要判断是否已经投过票了
	if (rf.votedFor == -1 || rf.votedFor == args.CandidateId) && uptoDate {
		rf.grantedvoteCh <- struct{}{}
		rf.state = STATE_FLLOWER
		reply.VoteGranted = true
		rf.votedFor = args.CandidateId
	}
}

func (rf *Raft) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) {
	// Your code here.
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()

	reply.Success = false
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.NextIndex = rf.getLastIndex() + 1
		return
	}

	// 传送心跳
	rf.heartbeatCh <- struct{}{}

	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = STATE_FLLOWER
		rf.votedFor = -1
	}
	reply.Term = args.Term

	if args.PrevLogIndex > rf.getLastIndex() {
		reply.NextIndex = rf.getLastIndex() + 1
		return
	}

	term := rf.log[args.PrevLogIndex].LogTerm
	if args.PrevLogTerm != term {
		for i := args.PrevLogIndex - 1 ; i >= 0; i-- {
			if rf.log[i].LogTerm != term {
				reply.NextIndex = i + 1
				break
			}
		}
		return
	}
	
	rf.log = rf.log[: args.PrevLogIndex+1]
	rf.log = append(rf.log, args.Entries...)

	reply.Success = true
	reply.NextIndex = rf.getLastIndex() + 1

	if args.LeaderCommit > rf.commitIndex {
		last := rf.getLastIndex()
		if args.LeaderCommit > last {
			rf.commitIndex = last
		} else {
			rf.commitIndex = args.LeaderCommit
		}
		rf.commitCh <- struct{}{}
	}
	return
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// returns true if labrpc says the RPC was delivered.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args RequestVoteArgs, reply *RequestVoteReply) bool {
	// 调用RPC
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if ok {
		term := rf.currentTerm
		if rf.state != STATE_CANDIDATE {
			return ok
		}
		if args.Term != term {
			return ok
		}

		// 更新自己的term
		if reply.Term > term {
			rf.currentTerm = reply.Term
			rf.state = STATE_FLLOWER
			rf.votedFor = -1
			rf.persist()
		}
		if reply.VoteGranted {
			rf.voteAcquired++
			if rf.state == STATE_CANDIDATE && rf.voteAcquired > len(rf.peers)/2 {
				rf.state = STATE_FLLOWER
				rf.leaderCh <- struct{}{}
			}
		}
	}
	return ok
}

func (rf *Raft) sendAppendEntries(server int, args AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if ok {
		if rf.state != STATE_LEADER {
			return ok
		}
		if args.Term != rf.currentTerm {
			return ok
		}

		// 更新自己的term
		if reply.Term > rf.currentTerm {
			rf.currentTerm = reply.Term
			rf.state = STATE_FLLOWER
			rf.votedFor = -1
			rf.persist()
			return ok
		}
		if reply.Success {
			if len(args.Entries) > 0 {
				rf.nextIndex[server] = reply.NextIndex
				rf.matchIndex[server] = rf.nextIndex[server] - 1
			}
		} else {
			rf.nextIndex[server] = reply.NextIndex
		}
	}
	return ok
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	index := -1
	term := rf.currentTerm
	isLeader := rf.state == STATE_LEADER

	if isLeader {
		index = rf.getLastIndex() + 1
		rf.log = append(rf.log, LogEntry{LogTerm:term,LogComd:command}) // append new entry from client
		rf.persist()
	}
	return index, term, isLeader
}

//
// the tester calls Kill() when a Raft instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (rf *Raft) Kill() {
	// Your code here, if desired.
}

func (rf *Raft) boatcastRequestVote() {
	var args RequestVoteArgs
	rf.mu.Lock()
	// 初始化要传给RPC调用的函数参数
	args.Term = rf.currentTerm
	args.CandidateId = rf.me
	args.LastLogTerm = rf.getLastTerm()
	args.LastLogIndex = rf.getLastIndex()
	rf.mu.Unlock()

	// 遍历整个Raft服务器数组，执行领导人选举
	for i := range rf.peers {
		if i != rf.me && rf.state == STATE_CANDIDATE {
			go func(i int) {
				var reply RequestVoteReply
				rf.sendRequestVote(i, args, &reply)
			}(i)
		}
	}
}

func (rf *Raft) boatcastAppendEntries() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	N := rf.commitIndex
	last := rf.getLastIndex()
	for i := rf.commitIndex + 1; i <= last; i++ {
		num := 1
		for j := range rf.peers {
			if j != rf.me && rf.matchIndex[j] >= i && rf.log[i].LogTerm == rf.currentTerm {
				// fmt.Printf("who agree = %v\n", j)
				num++
			}
		}
		if 2*num > len(rf.peers) {
			N = i
			// fmt.Printf("rf.me = %v, count = %v, len(peers) = %v\n", rf.me, count, len(rf.peers))
		}
	}
	if N != rf.commitIndex {
		rf.commitIndex = N
		rf.commitCh <- true
	}

	for i := range rf.peers {
		if i != rf.me && rf.state == STATE_LEADER {
			var args AppendEntriesArgs
			// 参数初始化
			args.Term = rf.currentTerm
			args.LeaderId = rf.me
			args.PrevLogIndex = rf.nextIndex[i] - 1
			args.PrevLogTerm = rf.log[args.PrevLogIndex].LogTerm
			args.Entries = make([]LogEntry, len(rf.log[args.PrevLogIndex + 1:]))
			copy(args.Entries, rf.log[args.PrevLogIndex + 1:])

			args.LeaderCommit = rf.commitIndex
			go func(i int,args AppendEntriesArgs) {
				var reply AppendEntriesReply
				rf.sendAppendEntries(i, args, &reply)
			}(i,args)
		}
	}
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
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
	rf.state = STATE_FLLOWER
	rf.votedFor = -1
	rf.log = append(rf.log, LogEntry{LogTerm: 0})
	rf.currentTerm = 0
	rf.heartbeatCh = make(chan interface{}, 100)
	rf.leaderCh = make(chan interface{}, 100)
	rf.commitCh = make(chan interface{}, 100)
	rf.grantedvoteCh = make(chan interface{}, 100)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// 状态机循环
	go func() {
		for {
			switch rf.state {
			case STATE_FLLOWER:
				select {
				// 接收到心跳或者投票确认就重新计时
				case <-rf.heartbeatCh:
				case <-rf.grantedvoteCh:
				// 超时就变为候选者
				case <-time.After(time.Duration(rand.Int63() % 333 + 550) * time.Millisecond):
					rf.state = STATE_CANDIDATE
				}
			case STATE_LEADER:
				// 如果是领导人就执行复制日志(携带心跳)的操作
				rf.boatcastAppendEntries()
				time.Sleep(50 * time.Millisecond)
			case STATE_CANDIDATE:
				// 候选人要准备投票选举
				rf.mu.Lock()
				rf.currentTerm++
				// 先投给自己一票
				rf.votedFor = rf.me
				rf.voteAcquired = 1
				rf.persist()
				rf.mu.Unlock()

				go rf.boatcastRequestVote()

				select {
				case <-time.After(time.Duration(rand.Int63() % 333 + 550) * time.Millisecond):
				// 接收到心跳(其他节点先成为领导人)候选人就变为跟随者
				case <-rf.heartbeatCh:
					rf.state = STATE_FLLOWER
				// 从leaderCh通道接收到消息说明赢得选举，成为领导人
				case <-rf.leaderCh:
					rf.mu.Lock()
					rf.state = STATE_LEADER
					// fmt.Printf("leader = %v currentTerm = %v\n", rf.me, rf.currentTerm)

					// 初始化这两个只有leader用的数组
					rf.nextIndex = make([]int,len(rf.peers))
					rf.matchIndex = make([]int,len(rf.peers))
					for i := range rf.peers {
						rf.nextIndex[i] = rf.getLastIndex() + 1
						rf.matchIndex[i] = 0
					}
					rf.mu.Unlock()
				}
			}
		}
	}()

	go func() {
		for {
			select {
			// 日志提交
			case <-rf.commitCh:
				rf.mu.Lock()
			  	commitIndex := rf.commitIndex
				for i := rf.lastApplied+1; i <= commitIndex; i++ {
					msg := ApplyMsg{Index: i, Command: rf.log[i].LogComd}
					// fmt.Printf("leader = %v, me = %v, msg.Index = %v, msg.Command = %v\n", rf.state==STATE_LEADER, rf.me, msg.Index, msg.Command)
					applyCh <- msg
					rf.lastApplied = i
				}
				rf.mu.Unlock()
			}
		}
	}()
	return rf
}
