# 基于Raft的键值服务

+ 客户发送`Put()`、`Append()`、`Get()`RPCs给键值服务器
+ 一个客户可以发送一个RPC给任意一个键值服务器，即使该服务器不是`leader`也不必重新发送。
如果该操作已经被提交到Raft日志里了，则报告给客户。如果该操作提交失败了，则报告给客户让其重试其他服务器。
+ 服务器只能通过Raft日志来相互交流。

## PartA：键值服务
1. 先完成`server.go`中的`Op`结构体。

```go
type Op struct {
	// "Get" "Put" "Append"
	Type 	string
	Key 	string
	Value 	string
	// RaftKV的Id
	Id 		int64
	// 指令Id，用来标识每一条指令，防止指令重复执行
	ReqId  	int
}
```

2. 完成`server.go`中的`RaftKV`结构体

```go
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
```

3. `server.go`里的主循环：`StartKVServer()`

```go
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *RaftKV {
	// call gob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	gob.Register(Op{})

	kv := new(RaftKV)
	kv.me = me
	kv.maxraftstate = maxraftstate

	// Your initialization code here.

	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	kv.db = make(map[string]string)
	kv.ack = make(map[int64]int)
	kv.result = make(map[int]chan Op)

	// 由于该函数要快速返回，所以使用goroutine
	go func() {
		for {
			// 从Raft中传来的日志
			msg := <- kv.applyCh
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
				// 如果通道存在就传送请求op
				ch <- op
			} else {
				kv.result[msg.Index] = make(chan Op, 1)
			}
			
			kv.mu.Unlock()
		}
	}()

	return kv
}
```

4. `command.go`里的结构体添加成员

```go
// Put or Append
type PutAppendArgs struct {
	// You'll have to add definitions here.
	Key   string
	Value string
	Op    string // "Put" or "Append"
	// You'll have to add definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Id		int64
	ReqID	int	
}

type GetArgs struct {
	Key 	string
	// You'll have to add definitions here.
	Id		int64
	ReqID	int	
}
```

5. `server.go`里提供给client的RPC操作：`Get`和`PutAppend`

这两个实现都很简单，都是先构造`Op`对象，然后与将该指令传到底层的`Raft`，等待主循环中的回应。

```go
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
```

6. `client.go`

client的实现很简单

```go
type Clerk struct {
	servers []*labrpc.ClientEnd
	// You will have to modify this struct.
	id 		int64
	// 唯一的指令Id
	reqid	int
	// 保护reqid
	mu 		sync.Mutex
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	// You'll have to add code here.
	ck.id = nrand()
	ck.reqid = 0
	return ck
}
```

之后就是`Get`和`PutAppend`操作，直接构造`args`然后通过调用server的RPC传入该参数即可。

```go
func (ck *Clerk) Get(key string) string {
	// You will have to modify this function.
	var args GetArgs
	args.Key = key
	args.Id = ck.id

	ck.mu.Lock()
	args.ReqID = ck.reqid
	ck.reqid++
	ck.mu.Unlock()

	for {
		for _, v := range ck.servers {
			var reply GetReply
			ok := v.Call("RaftKV.Get", &args, &reply)
			if ok && reply.WrongLeader == false {
				return reply.Value
			}
		}
	}
}

func (ck *Clerk) PutAppend(key string, value string, op string) {
	// You will have to modify this function.
	var args PutAppendArgs
	args.Key = key
	args.Value = value
	args.Op = op
	args.Id = ck.id

	ck.mu.Lock()
	args.ReqID = ck.reqid
	ck.reqid++
	ck.mu.Unlock()

	for {
		for _, v := range ck.servers {
			var reply PutAppendReply
			ok := v.Call("RaftKV.PutAppend", &args, &reply)
			if ok && reply.WrongLeader == false {
				return 
			}
		}
	}
}
```

## 测试
能通过快照之前所有的测试：
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
2018/11/19 13:37:31 Warning: client 0 managed to perform only 6 put operations in 1 sec?
  ... Passed
Test: many partitions, many clients ...
  ... Passed
Test: persistence with one client ...
  ... Passed
Test: persistence with concurrent clients ...
  ... Passed
Test: persistence with concurrent clients, unreliable ...
  ... Passed
Test: persistence with concurrent clients and repartitioning servers...
2018/11/19 13:39:23 Warning: client 2 managed to perform only 9 put operations in 1 sec?
  ... Passed
Test: persistence with concurrent clients and repartitioning servers, unreliable...
2018/11/19 13:39:51 Warning: client 0 managed to perform only 8 put operations in 1 sec?
2018/11/19 13:40:12 Warning: client 3 managed to perform only 8 put operations in 1 sec?
  ... Passed
```
