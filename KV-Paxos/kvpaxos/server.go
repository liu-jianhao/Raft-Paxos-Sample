package kvpaxos

import (
	"net"
	"time"
)
import "fmt"
import "net/rpc"
import "log"
import "../paxos"
import "sync"
import "sync/atomic"
import "os"
import "syscall"
import "encoding/gob"
import "math/rand"


const Debug = 0

// 请求记录的时间，一段时间后需要清理
const TimetoLive = 10

const TimeInterval = 100 * time.Millisecond

func initTime() time.Duration {
	return TimeInterval
}

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}


type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	OpId int64
	Op string
	Key string
	Value string
}

type KVPaxos struct {
	mu         sync.Mutex
	l          net.Listener
	me         int
	dead       int32 // for testing
	unreliable int32 // for testing
	px         *paxos.Paxos

	// Your definitions here.
	// 轮次
	seq int
	// 请求记录
	requests 	map[int64]int
	// 请求对应的回应记录
	replies 	map[int64]interface{}
	// 实际的kv存储
	kvstore 	map[string]string
}

func (kv *KVPaxos) removeDup(opid int64) (interface{}, bool) {
	_, ok := kv.requests[opid]
	if !ok {
		return nil, false
	}
	kv.requests[opid] = TimetoLive
	return kv.replies[opid], true
}

func (kv *KVPaxos) recordOp(opid int64, reply interface{}) {
	kv.requests[opid] = TimetoLive
	kv.replies[opid] = reply
}

func (kv *KVPaxos) sync(myOp *Op) {
	// 先保存当前的轮次
	seq := kv.seq

	DPrintf("----- server %d sync %v\n", kv.me, myOp)

	wait := initTime()
	for {
		fate, v := kv.px.Status(seq)
		// Decided说明该轮次已经得出结果，Pending说明还没开始
		if fate == paxos.Decided {
			returnOp := v.(Op)
			DPrintf("----- server %d : seq %d : %v\n", kv.me, seq, returnOp)
			// 如果相等，说明“我”提的提案被选中了，否则选中的是别人的提案，因此执行别人的提案操作
			if returnOp.OpId == myOp.OpId {
				break
			} else if returnOp.Op == Put || returnOp.Op == Append {
				kv.doPutAppend(returnOp.Op, returnOp.Key, returnOp.Value)
				kv.recordOp(returnOp.OpId, &PutAppendReply{OK})
			} else {
				value, ok := kv.doGet(returnOp.Key)
				if ok {
					kv.recordOp(returnOp.OpId, &GetReply{OK, value})
				} else {
					kv.recordOp(returnOp.OpId, &GetReply{ErrNoKey, ""})
				}
			}

			kv.px.Done(seq)
			seq++
			wait = initTime()
		} else if fate == paxos.Pending {
			// 启动状态机
			kv.px.Start(seq, *myOp)
			time.Sleep(wait)
			if wait < 1 * time.Second {
				wait *= 2
			}
		} else {
			DPrintf("--- bug :( ---\n")
		}
	}

	kv.px.Done(seq)
	kv.seq = seq + 1
}

func (kv *KVPaxos) doGet(key string) (string, bool) {
	v, ok := kv.kvstore[key]
	if ok {
		DPrintf("Get : server %d : key %s : value %s\n", kv.me, key, v)
	} else {
		DPrintf("Get : server %d : key %s : ErrNoKey\n", kv.me, key)
	}
	return v, ok
}

func (kv *KVPaxos) Get(args *GetArgs, reply *GetReply) error {
	// Your code here.
	kv.mu.Lock()
	defer kv.mu.Unlock()

	DPrintf("RPC Get : server %d : key %s\n", kv.me, args.Key)

	rp, yes := kv.removeDup(args.OpId)
	if yes {
		reply.Value = rp.(*GetReply).Value
		reply.Err = rp.(*GetReply).Err
		return nil
	}

	op := Op{
		OpId: 	args.OpId,
		Op:		Get,
		Key:	args.Key,
		Value: 	"",
	}

	kv.sync(&op)
	v, ok := kv.doGet(op.Key)
	if ok {
		reply.Err = OK
		reply.Value = v
	} else {
		reply.Err = ErrNoKey
	}

	kv.recordOp(args.OpId, reply)

	return nil
}

func (kv *KVPaxos) doPutAppend(op string, key string, value string) {
	DPrintf("%s server %d : key %s : value %s\n", op, kv.me, key, value)
	if op == Put {
		kv.kvstore[key] = value
	} else {
		kv.kvstore[key] += value
		DPrintf("------------------------> %s\n", kv.kvstore[key])
	}
}

func (kv *KVPaxos) PutAppend(args *PutAppendArgs, reply *PutAppendReply) error {
	// Your code here.
	kv.mu.Lock()
	defer kv.mu.Unlock()

	DPrintf("RPC PutAppend : server %d : op %s : val %s\n", kv.me, args.Op, args.Key, args.Value)

	rp, yes := kv.removeDup(args.OpId)
	if yes {
		reply.Err = rp.(*PutAppendReply).Err
		return nil
	}

	op := Op{
		OpId: 	args.OpId,
		Op:		args.Op,
		Key:	args.Key,
		Value:	args.Value,
	}

	kv.sync(&op)
	kv.doPutAppend(op.Op, op.Key, op.Value)
	reply.Err = OK

	kv.recordOp(args.OpId, reply)

	return nil
}

func (kv *KVPaxos) clearRequests() {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	for id := range kv.requests {
		kv.requests[id]--
		if kv.requests[id] <= 0 {
			delete(kv.requests, id)
			delete(kv.replies, id)
		}
	}
}

// tell the server to shut itself down.
// please do not change these two functions.
func (kv *KVPaxos) kill() {
	DPrintf("Kill(%d): die\n", kv.me)
	atomic.StoreInt32(&kv.dead, 1)
	kv.l.Close()
	kv.px.Kill()
}

// call this to find out if the server is dead.
func (kv *KVPaxos) isdead() bool {
	return atomic.LoadInt32(&kv.dead) != 0
}

// please do not change these two functions.
func (kv *KVPaxos) setunreliable(what bool) {
	if what {
		atomic.StoreInt32(&kv.unreliable, 1)
	} else {
		atomic.StoreInt32(&kv.unreliable, 0)
	}
}

func (kv *KVPaxos) isunreliable() bool {
	return atomic.LoadInt32(&kv.unreliable) != 0
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Paxos to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
//
func StartServer(servers []string, me int) *KVPaxos {
	// call gob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	gob.Register(Op{})

	kv := new(KVPaxos)
	kv.me = me

	// Your initialization code here.
	kv.requests = make(map[int64]int)
	kv.replies = make(map[int64]interface{})
	kv.kvstore = make(map[string]string)

	rpcs := rpc.NewServer()
	rpcs.Register(kv)

	kv.px = paxos.Make(servers, me, rpcs)

	os.Remove(servers[me])
	l, e := net.Listen("unix", servers[me])
	if e != nil {
		log.Fatal("listen error: ", e)
	}
	kv.l = l


	// please do not change any of the following code,
	// or do anything to subvert it.

	go func() {
		for kv.isdead() == false {
			conn, err := kv.l.Accept()
			if err == nil && kv.isdead() == false {
				if kv.isunreliable() && (rand.Int63()%1000) < 100 {
					// discard the request.
					conn.Close()
				} else if kv.isunreliable() && (rand.Int63()%1000) < 200 {
					// process the request but force discard of reply.
					c1 := conn.(*net.UnixConn)
					f, _ := c1.File()
					err := syscall.Shutdown(int(f.Fd()), syscall.SHUT_WR)
					if err != nil {
						fmt.Printf("shutdown: %v\n", err)
					}
					go rpcs.ServeConn(conn)
				} else {
					go rpcs.ServeConn(conn)
				}
			} else if err == nil {
				conn.Close()
			}
			if err != nil && kv.isdead() == false {
				fmt.Printf("KVPaxos(%v) accept: %v\n", me, err.Error())
				kv.kill()
			}
		}
	}()

	go func() {
		for kv.isdead() == false {
			time.Sleep(TimeInterval)
			kv.clearRequests()
		}
	}()

	return kv
}
