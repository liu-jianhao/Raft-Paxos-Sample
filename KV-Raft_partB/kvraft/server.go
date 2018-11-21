package raftkv

import (
	"bytes"
	"encoding/gob"
	"labrpc"
	"log"
	"raft"
	"sync"
	"time"
)

const Debug = 0

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
	// "Get" "Put" "Append"
	Type 	string
	Key 	string
	Value 	string
	// RaftKV的Id
	Id 		int64
	// 指令Id，用来标识每一条指令，防止指令重复执行
	ReqId  	int
}

type RaftKV struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	maxraftstate int // snapshot if log grows this big

	// Your definitions here.
	// 数据库，存储数据
	db 		map[string]string
	// map[Id]ReqId
	ack		map[int64]int 
	// map[msg.Index]chan Op
	result 	map[int]chan Op
}

// 添加指令到底层Raft的日志
func (kv *RaftKV) AppendEntryToLog(entry Op) bool {
	index, _, isLeader := kv.rf.Start(entry)
	if !isLeader {
		return false
	}

	kv.mu.Lock()
	ch, ok := kv.result[index]
	if !ok {
		ch = make(chan Op, 1)
		kv.result[index] = ch
	}
	kv.mu.Unlock()

	select {
	// 如果相等说明指令成功写入Raft日志且已经提交了
	case op := <-ch:
		return op == entry
	// 超时
	case <- time.After(1000 * time.Millisecond):
		return false
	}
}


func (kv *RaftKV) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	op := Op{
		Type: 	"Get",
		Key:	args.Key,
		Id:		args.Id,
		ReqId:	args.ReqID,
	}

	ok := kv.AppendEntryToLog(op)
	if ok {
		reply.WrongLeader = false
		reply.Err = OK
		
		// 从db中取值
		reply.Value = kv.db[args.Key]
	} else {
		reply.WrongLeader = true
	}
}

func (kv *RaftKV) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	op := Op{
		Type: 	args.Op,
		Key:	args.Key,
		Value:	args.Value,
		Id:		args.Id,
		ReqId:	args.ReqID,
	}

	ok := kv.AppendEntryToLog(op)
	if ok {
		reply.WrongLeader = false
		reply.Err = OK
	} else {
		reply.WrongLeader = true
	}
}

//
// the tester calls Kill() when a RaftKV instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (kv *RaftKV) Kill() {
	kv.rf.Kill()
	// Your code here, if desired.
}

//
// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots with persister.SaveSnapshot(),
// and Raft should save its state (including log) with persister.SaveRaftState().
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
// StartKVServer() must return quickly, so it should start goroutines
// for any long-running work.
//
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *RaftKV {
	// call gob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	gob.Register(Op{})

	kv := new(RaftKV)
	kv.me = me
	kv.maxraftstate = maxraftstate

	// Your initialization code here.

	kv.applyCh = make(chan raft.ApplyMsg, 100)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	kv.db = make(map[string]string)
	kv.ack = make(map[int64]int)
	kv.result = make(map[int]chan Op)

	// 由于该函数要快速返回，所以使用goroutine
	go func() {
		for {
			// 从Raft中传来的日志
			msg := <- kv.applyCh

			if msg.UseSnapshot {
				var LastIncludedIndex int
				var LastIncludedTerm int

				b := bytes.NewBuffer(msg.Snapshot)
				d := gob.NewDecoder(b)

				kv.mu.Lock()
				d.Decode(&LastIncludedIndex)
				d.Decode(&LastIncludedTerm)
				kv.db = make(map[string]string)
				kv.ack = make(map[int64]int)
				d.Decode(&kv.db)
				d.Decode(&kv.ack)
				kv.mu.Unlock()
			} else {
				// .(type)是类型断言，将接口类型的值Command转换成Op
				op := msg.Command.(Op)

				kv.mu.Lock()
				
				v, ok := kv.ack[op.Id]
				// 检查ack[op.Id]中是否已经有值，如果有值，请求的值比之前的ReqId大就说明需要写入db
				if !(ok && v >= op.ReqId) {
					if op.Type == "Put" {
						kv.db[op.Key] = op.Value
					} else if op.Type == "Append" {
						kv.db[op.Key] += op.Value
					}

					// 更新ReqId
					kv.ack[op.Id] = op.ReqId
				}

				ch, ok := kv.result[msg.Index]
				if ok {
					// select {
					// case <-kv.result[msg.Index]:
					// default:
					// }
					// 如果通道存在就传送请求op
					ch <- op
				} else {
					kv.result[msg.Index] = make(chan Op, 1)
				}

				// 超过阀值，需要创建快照
				if maxraftstate != -1 && kv.rf.GetRaftStateSize() > maxraftstate {
					w := new(bytes.Buffer)	
					e := gob.NewEncoder(w)
					e.Encode(kv.db)
					e.Encode(kv.ack)
					data := w.Bytes()
					go kv.rf.StartSnapshot(data, msg.Index)
				}
				
				kv.mu.Unlock()
			}
		}
	}()

	return kv
}
