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
//   ask a Raft for its currenT term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import "sync"
import "labrpc"
import "time"
import "math/rand"

// import "bytes"
// import "encoding/gob"

// 服务器的三种状态
const (
	STATE_FLLOWER = 0
	STATE_CANDIDATE = 1
	STATE_LEADER = 2
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

	/*
	 * 全部服务器上面的可持久化状态:
	 *  currentTerm 	服务器看到的最近Term(第一次启动的时候为0,后面单调递增)
	 *  votedFor     	当前Term收到的投票候选 (如果没有就为null)
	 *  log[]        	日志项; 每个日志项包含机器状态和被leader接收的Term(first index is 1)
	 */
	currentTerm int
	votedFor int

		
	/*
	 * 全部服务器上面的不稳定状态:
	 *	commitIndex 	已经被提交的最新的日志索引(第一次为0,后面单调递增)
	 *	lastApplied      已经应用到服务器状态的最新的日志索引(第一次为0,后面单调递增)
	*/
	commitIndex int
	lastApplied int

	/*
	 * leader上面使用的不稳定状态（完成选举之后需要重新初始化）
	 *	nextIndex[]	
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
}

type AppendEntriesReply struct {
	// currentTerm，用来给领导人更新term
	Term int
	// true，说明和跟随者的日志匹配
	Success bool
	// NextIndex int
}

//
// example RequestVote RPC handler.
//
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
		// rf.votedFor = -1
		// 投票
		reply.VoteGranted = true
	}

}


func (rf *Raft) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm

	if args.Term < rf.currentTerm {
		reply.Success = false
		return
	}
	reply.Success = true	
	// 传送心跳
	rf.heartbeatCh <- struct{}{}
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

		// if reply.Success == true {

		// }
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

func (rf *Raft) broadcastAppendEntries() {
	// rf.mu.Lock()
	// defer rf.mu.Unlock()

	for i := range rf.peers {
		if i != rf.me && rf.state == STATE_LEADER {
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