# KV-Paxos
这次实验是在之前的实现Paxos基础上实现kv存储。

## 实验准备
对之前的实验的paxos的几个接口要知道怎么用，kvpaxos利用paxos来选中一个提案。

### 记录每次客户请求：Op结构体
```go
type Op struct {
	OpId int64
	Op string
	Key string
	Value string
}
```

### 键值服务器：kvPaxos结构体
```go
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
```

### 实现逻辑
1. 假设客户有一个操作请求给其中一个kvpaxos服务器
2. kvpaxos就需要将这个请求交个paxos(px.Start())
3. 每隔一段时间检查该操作的结果(px.Status())
4. 如果返回的结果是(Decided)并且返回的操作和该kvpaxos提出的操作一致(如果返回的结果还是每个操作都会有一个OpId来唯一标识一个操作)，
说明该kvpaxos的操作被选中了
5. 如果与该kvpaxos提出的操作不一致，说明其他kvpaxos提出的操作被选中了，该kvpaxos就需要执行该操作来达成一致，
6. 由于一个操作可能被重复请求，这就需要记录请求防止重复执行


这次实验的难点就是实现上面的2-5步，只要理解了也就不难了。
```go
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
```


## 运行测试
```
go test
Test: Basic put/append/get ...
  ... Passed
Test: Concurrent clients ...
  ... Passed
Test: server frees Paxos log memory...
  ... Passed
Test: No partition ...
  ... Passed
Test: Progress in majority ...
  ... Passed
Test: No progress in minority ...
  ... Passed
Test: Completion after heal ...
  ... Passed
Test: Sequence of puts, unreliable ...
  ... Passed
Test: Concurrent clients, unreliable ...nexpected EOF
  ... Passed
Test: Concurrent Append to same key, unreliable ...
  ... Passed
Test: Tolerates holes in paxos sequence ...
  ... Passed
Test: Many clients, changing partitions ...
  ... Passed
PASS
ok      _/home/liu/Desktop/programing/go/src/Raft-Paxos-Sample/KV-Paxos/kvpaxos 155.256s
```

## 总结
和kvraft的A部分的实现差不多
