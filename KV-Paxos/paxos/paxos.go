package paxos

//
// Paxos library, to be included in an application.
// Multiple applications will run, each including
// a Paxos peer.
//
// Manages a sequence of agreed-on values.
// The set of peers is fixed.
// Copes with network failures (partition, msg loss, &c).
// Does not store anything persistently, so cannot handle crash+restart.
//
// The application interface:
//
// px = paxos.Make(peers []string, me int)
// px.Start(seq int, v interface{}) -- start agreement on new instance
// px.Status(seq int) (Fate, v interface{}) -- get info about an instance
// px.Done(seq int) -- ok to forget all instances <= seq
// px.Max() int -- highest instance seq known, or -1
// px.Min() int -- instances before this seq have been forgotten
//

import "net"
import "net/rpc"
import "log"

import "os"
import "syscall"
import "sync"
import "sync/atomic"
import "fmt"
import "math/rand"
import "time"
import "strconv"


// px.Status() return values, indicating
// whether an agreement has been decided,
// or Paxos has not yet reached agreement,
// or it was agreed but forgotten (i.e. < Min()).
type Fate int

const (
	Decided   Fate = iota + 1
	Pending        // not yet decided.
	Forgotten      // decided but forgotten.
)

type instance struct{
	state 		Fate
	proposeNum	string
	acceptNum	string
	acceptValue	interface{}
}

type Paxos struct {
	mu         sync.Mutex
	l          net.Listener
	dead       int32 // for testing
	unreliable int32 // for testing
	rpcCount   int32 // for testing
	peers      []string
	me         int // index into peers[]


	// Your data here.
	// 保存每个服务器节点的已经Done的最高seq
	dones 		[]int
	// 保存每一轮的状态值
	instances 	map[int]instance
}

//
// call() sends an RPC to the rpcname handler on server srv
// with arguments args, waits for the reply, and leaves the
// reply in reply. the reply argument should be a pointer
// to a reply structure.
//
// the return value is true if the server responded, and false
// if call() was not able to contact the server. in particular,
// the replys contents are only valid if call() returned true.
//
// you should assume that call() will time out and return an
// error after a while if it does not get a reply from the server.
//
// please use call() to send all RPCs, in client.go and server.go.
// please do not change this function.
//
func call(srv string, name string, args interface{}, reply interface{}) bool {
	c, err := rpc.Dial("unix", srv)
	if err != nil {
		err1 := err.(*net.OpError)
		if err1.Err != syscall.ENOENT && err1.Err != syscall.ECONNREFUSED {
			//fmt.Printf("paxos Dial() failed: %v\n", err1)
		}
		return false
	}
	defer c.Close()

	err = c.Call(name, args, reply)
	if err == nil {
		return true
	}

	//fmt.Println(err)
	return false
}


type PrepareArgs struct {
	Seq			int
	ProposeNum	string 	
}

type PrepareReply struct {
	Success			bool
	AcceptPNum		string
	AcceptPValue	interface{}
}

type AcceptArgs struct {
	Seq				int
	AcceptPNum 		string
	AcceptPValue	interface{}
}

type AcceptReply struct {
	Success			bool
}

type DecideArgs struct {
	Seq 		int
	ProposeNum	string
	Value		interface{}
	Me 			int
	Done 		int
}

type DecideReply struct {
	Success 	bool
}

func (px *Paxos) Prepare(args PrepareArgs, reply *PrepareReply) error {
	px.mu.Lock()
	defer px.mu.Unlock()

	ins, exist := px.instances[args.Seq]
	if exist {
		// 存在就比较编号大小，只接受编号大于之前的
		if args.ProposeNum > ins.proposeNum {
			reply.Success = true
		} else {
			reply.Success = false
		}
	} else {
		// 不存在就创建一个
		ins = instance {
			state: Pending,
		}
		px.instances[args.Seq] = ins
		reply.Success = true
	}

	if reply.Success {
		reply.AcceptPNum = ins.acceptNum
		reply.AcceptPValue = ins.acceptValue

		// 更新该轮的状态
		tmp := px.instances[args.Seq]
		tmp.proposeNum = args.ProposeNum
		px.instances[args.Seq] = tmp
	}

	return nil
}

func (px *Paxos) Accept(args AcceptArgs, reply *AcceptReply) error {
	px.mu.Lock()
	defer px.mu.Unlock()

	ins, exist := px.instances[args.Seq]
	if exist {
		// 存在就比较编号大小，与Prepare不同，还可以接受等于的
		if args.AcceptPNum >= ins.proposeNum {
			reply.Success = true
		} else {
			reply.Success = false
		}
	} else {
		// 不存在就创建一个
		ins = instance {
			state: Pending,
		}
		px.instances[args.Seq] = ins
		reply.Success = true
	}

	if reply.Success {
		// 更新该轮的状态
		tmp := px.instances[args.Seq]
		tmp.proposeNum = args.AcceptPNum
		tmp.acceptNum = args.AcceptPNum
		tmp.acceptValue = args.AcceptPValue
		px.instances[args.Seq] = tmp
	}

	return nil
}

func (px *Paxos) Decide(args DecideArgs, reply *DecideReply) error {
	px.mu.Lock()
	defer px.mu.Unlock()

	ins, exist := px.instances[args.Seq]
	if exist {
		// 更新该轮的状态
		tmp := px.instances[args.Seq]
		tmp.proposeNum = args.ProposeNum
		tmp.acceptNum = args.ProposeNum
		tmp.acceptValue = args.Value
		tmp.state = Decided
		px.instances[args.Seq] = tmp
		px.dones[args.Me] = args.Done
	} else {
		ins = instance {
			state: Pending,
		}
		px.instances[args.Seq] = ins
	}

	return nil
}


// 生成一个propose编号
func (px *Paxos) generatePNum() string {
	begin := time.Date(2018, time.November, 17, 22, 0, 0, 0, time.UTC)
	duration := time.Now().Sub(begin)
	return strconv.FormatInt(duration.Nanoseconds(), 10) + "-" + strconv.Itoa(px.me)
}


func (px *Paxos) sendPrepare(seq int, v interface{}) (bool, string, interface{}) {
	pNum := px.generatePNum()

	args := PrepareArgs {
		Seq: 		seq,
		ProposeNum:	pNum,
	}

	//fmt.Printf("PrepareArgs{ Seq: %v, ProposeNum: %v }\n", seq, pNum)

	replyPNum := ""
	num := 0
	for i, peer := range px.peers {
		var reply PrepareReply
		if i == px.me {
			px.Prepare(args, &reply)
		} else {
			call(peer, "Paxos.Prepare", args, &reply)
		}

		if reply.Success {
			num++

			if reply.AcceptPNum > replyPNum {
				replyPNum = reply.AcceptPNum
				v = reply.AcceptPValue
			}
		}
	}

	return num > len(px.peers)/2, pNum, v
}

func (px *Paxos) sendAccept(seq int, pNum string, v interface{}) bool {
	args := AcceptArgs {
		Seq: 		seq,
		AcceptPNum:	pNum,
		AcceptPValue: v,
	}

	//fmt.Printf("AcceptArgs{ Seq: %v, AcceptPNum: %v, AcceptPValue: %v }\n", seq, pNum, v)

	num := 0
	for i, peer := range px.peers {
		var reply AcceptReply
		if i == px.me {
			px.Accept(args, &reply)
		} else {
			call(peer, "Paxos.Accept", args, &reply)
		}

		if reply.Success {
			num++
		}
	}

	return num > len(px.peers)/2
}

func (px *Paxos) sendDecide(seq int, pNum string, v interface{}) {
	px.mu.Lock()
	tmp := px.instances[seq]
	tmp.state = Decided	
	tmp.proposeNum = pNum
	tmp.acceptNum = pNum
	tmp.acceptValue = v
	px.instances[seq] = tmp
	px.mu.Unlock()

	//fmt.Printf("instance{ state: %v, proposeNum: %v, acceptNum: %v, acceptValue: %v }\n",
	//		tmp.state, tmp.proposeNum, tmp.acceptNum, tmp.acceptValue)

	args := DecideArgs {
		Seq:	seq,
		ProposeNum: pNum,
		Value:	v,
		Me:		px.me,
		Done:	px.dones[px.me],
	}

	for i, peer := range px.peers {
		if i == px.me {
			continue
		}

		var reply DecideReply
		call(peer, "Paxos.Decide", args, &reply)
	}
}


//
// the application wants paxos to start agreement on
// instance seq, with proposed value v.
// Start() returns right away; the application will
// call Status() to find out if/when agreement
// is reached.
//
func (px *Paxos) Start(seq int, v interface{}) {
	// Your code here.
	//fmt.Printf("Paxos server Start, seq = %v, value = %v\n", seq, v)
	go func() {
		if seq < px.Min() {
			return
		}

		for {
			// 阶段一：Prepare，提案的生成及请求
			ok, pNum, pValue := px.sendPrepare(seq, v)
		
			if ok {
				// 阶段二：Accept，提案的批准
				ok = px.sendAccept(seq, pNum, pValue)
			}
		
			if ok {
				// 确认阶段，相当于Learner获取提案
				px.sendDecide(seq, pNum, pValue)
				break
			}

			// 获取当前状态
			state, _ := px.Status(seq)
			// 如果为Decided，说明该状态机的这一轮已经结束
			if state == Decided {
				break
			}
		}
	}()
}

//
// the application on this machine is done with
// all instances <= seq.
//
// see the comments for Min() for more explanation.
//
func (px *Paxos) Done(seq int) {
	// Your code here.
	px.mu.Lock()
	defer px.mu.Unlock()

	if seq > px.dones[px.me] {
		px.dones[px.me] = seq
	}
}

//
// the application wants to know the
// highest instance sequence known to
// this peer.
//
func (px *Paxos) Max() int {
	// Your code here.
	px.mu.Lock()
	defer px.mu.Unlock()

	max := 0
	for s := range px.instances {
		if s > max {
			max = s
		}
	}

	return max
}

//
// Min() should return one more than the minimum among z_i,
// where z_i is the highest number ever passed
// to Done() on peer i. A peers z_i is -1 if it has
// never called Done().
//
// Paxos is required to have forgotten all information
// about any instances it knows that are < Min().
// The point is to free up memory in long-running
// Paxos-based servers.
//
// Paxos peers need to exchange their highest Done()
// arguments in order to implement Min(). These
// exchanges can be piggybacked on ordinary Paxos
// agreement protocol messages, so it is OK if one
// peers Min does not reflect another Peers Done()
// until after the next instance is agreed to.
//
// The fact that Min() is defined as a minimum over
// *all* Paxos peers means that Min() cannot increase until
// all peers have been heard from. So if a peer is dead
// or unreachable, other peers Min()s will not increase
// even if all reachable peers call Done. The reason for
// this is that when the unreachable peer comes back to
// life, it will need to catch up on instances that it
// missed -- the other peers therefor cannot forget these
// instances.
//
func (px *Paxos) Min() int {
	// You code here.
	px.mu.Lock()
	defer px.mu.Unlock()

	min := px.dones[px.me]
	// fmt.Printf("min = %v\n", min)
	for i := range px.dones {
		if px.dones[i] < min {
			min = px.dones[i]
		}
	}

	for i, instance := range px.instances {
		if i <= min && instance.state == Decided {
			// 删除掉那些已经没用的状态
			delete(px.instances, i)
		}
	}

	return min + 1
}

//
// the application wants to know whether this
// peer thinks an instance has been decided,
// and if so what the agreed value is. Status()
// should just inspect the local peer state;
// it should not contact other Paxos peers.
//
func (px *Paxos) Status(seq int) (Fate, interface{}) {
	// Your code here.

	if seq < px.Min() {
		return Forgotten, nil
	}

	// 注意这里！！锁不能放在函数开头否则会死锁，因为Min中也会加锁
	px.mu.Lock()
	defer px.mu.Unlock()

	instance, exist := px.instances[seq]
	if !exist {
		return Pending, nil
	}

	return instance.state, instance.acceptValue
}



//
// tell the peer to shut itself down.
// for testing.
// please do not change these two functions.
//
func (px *Paxos) Kill() {
	atomic.StoreInt32(&px.dead, 1)
	if px.l != nil {
		px.l.Close()
	}
}

//
// has this peer been asked to shut down?
//
func (px *Paxos) isdead() bool {
	return atomic.LoadInt32(&px.dead) != 0
}

// please do not change these two functions.
func (px *Paxos) setunreliable(what bool) {
	if what {
		atomic.StoreInt32(&px.unreliable, 1)
	} else {
		atomic.StoreInt32(&px.unreliable, 0)
	}
}

func (px *Paxos) isunreliable() bool {
	return atomic.LoadInt32(&px.unreliable) != 0
}

//
// the application wants to create a paxos peer.
// the ports of all the paxos peers (including this one)
// are in peers[]. this servers port is peers[me].
//
func Make(peers []string, me int, rpcs *rpc.Server) *Paxos {
	px := &Paxos{}
	px.peers = peers
	px.me = me


	// Your initialization code here.
	//fmt.Printf("[Make] me = %d, len(px.peers) = %v\n", me, len(px.peers))
	px.dones = make([]int, len(px.peers))
	for i := range px.dones {
		px.dones[i] = -1
	}
	px.instances = make(map[int]instance)

	if rpcs != nil {
		// caller will create socket &c
		rpcs.Register(px)
	} else {
		rpcs = rpc.NewServer()
		rpcs.Register(px)

		// prepare to receive connections from clients.
		// change "unix" to "tcp" to use over a network.
		os.Remove(peers[me]) // only needed for "unix"
		l, e := net.Listen("unix", peers[me])
		if e != nil {
			log.Fatal("listen error: ", e)
		}
		px.l = l

		// please do not change any of the following code,
		// or do anything to subvert it.

		// create a thread to accept RPC connections
		go func() {
			for px.isdead() == false {
				conn, err := px.l.Accept()
				if err == nil && px.isdead() == false {
					if px.isunreliable() && (rand.Int63()%1000) < 100 {
						// discard the request.
						conn.Close()
					} else if px.isunreliable() && (rand.Int63()%1000) < 200 {
						// process the request but force discard of reply.
						c1 := conn.(*net.UnixConn)
						f, _ := c1.File()
						err := syscall.Shutdown(int(f.Fd()), syscall.SHUT_WR)
						if err != nil {
							fmt.Printf("shutdown: %v\n", err)
						}
						atomic.AddInt32(&px.rpcCount, 1)
						go rpcs.ServeConn(conn)
					} else {
						atomic.AddInt32(&px.rpcCount, 1)
						go rpcs.ServeConn(conn)
					}
				} else if err == nil {
					conn.Close()
				}
				if err != nil && px.isdead() == false {
					fmt.Printf("Paxos(%v) accept: %v\n", me, err.Error())
				}
			}
		}()
	}


	return px
}
