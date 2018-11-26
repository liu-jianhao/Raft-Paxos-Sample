# Paxos
这次实验目的是要实现Paxos算法。

## 实验框架

```go
px = paxos.Make(peers []string, me int)
px.Start(seq int, v interface{}) // start agreement on new instance
px.Status(seq int) (fate Fate, v interface{}) // get info about an instance
px.Done(seq int) // ok to forget all instances <= seq
px.Max() int // highest instance seq known, or -1
px.Min() int // instances before this have been forgotten
```

## Paxos的一些概念
### 什么是instance？
在论文《Paxos Made Simple》中的第三节实现状态机中提到的`instance`，其实这个`instance`就相当于`round`，就是“轮”的意思。

而每一轮都会选择一个提案，因此在Paxos结构体中我们要在每轮保存一些数据，这样才能实现状态机。

一轮中需要保存的数据：
```go
type instance struct{
	state 		Fate
	proposeNum	int
	AcceptNum	int
	AcceptValue	interface{}
}
```

Paxos结构体：
```go
type Paxos struct {
	mu         sync.Mutex
	l          net.Listener
	dead       int32 // for testing
	unreliable int32 // for testing
	rpcCount   int32 // for testing
	peers      []string
	me         int // index into peers[]

	// 保存每个服务器节点的已经Done的最高seq
	dones 		[]int
	// 保存每一轮的状态值
	instances 	map[int]instance
}
```

## Paxos状态机
```go
func (px *Paxos) Start(seq int, v interface{}) {
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
```

## 调试
1. 产生死锁？
程序直接就卡死在第一个测试，完全动不了，经调试及查看`test_test.go`文件的内容，发现程序是卡在`Min()`函数的`px.mu.Lock()`语句，一开始我以为是在`Start()`函数中调用的`Min()`，后来检查了好久，没发现有什么问题。后来看到`Status()`中调用到了`Min()`函数，而且这里在调用`Min()`之前还加了锁，连续两次锁定同一个锁当然会死锁。。。要将加锁放在调用`Min()`之后。

```go
func (px *Paxos) Status(seq int) (Fate, interface{}) {
	// Your code here.
	px.mu.Lock()
	defer px.mu.Unlock()
	
	if seq < px.Min() {
		return Forgotten, nil
	}

	instance, exist := px.instances[seq]
	if !exist {
		return Pending, nil
	}

	return instance.state, instance.acceptValue
}
```

2. rpc: can't find service Paxos.Prepare
不满足rpc的要求：
```
只有满足如下标准的方法才能用于远程访问，其余方法会被忽略：
- 方法是导出的
- 方法有两个参数，都是导出类型或内建类型
- 方法的第二个参数是指针
- 方法只有一个error接口类型的返回值

事实上，方法必须看起来像这样：
func (t *T) MethodName(argType T1, replyType *T2) error
```

3. 卡在accept阶段
是由于在`Accept`中把等于之前`proposeNum`的情况给认为是失败的，与之前`Prepare`不同，Prepare阶段是没有相同的`proposeNum`的。
```go
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
```

4. 成功
```
$ go test
Test: Single proposer ...
  ... Passed
Test: Many proposers, same value ...
  ... Passed
Test: Many proposers, different values ...
  ... Passed
Test: Out-of-order instances ...
  ... Passed
Test: Forgetting ...
  ... Passed
Test: Lots of forgetting ...
  ... Passed
Test: Paxos frees forgotten instance memory ...
  ... Passed
Test: Paxos Max() after Done()s ...
  ... Passed
Test: Many instances ...
  ... Passed
Test: Many instances, unreliable RPC ...
  ... Passed
Test: No decision if partitioned ...
  ... Passed
Test: Decision in majority partition ...
  ... Passed
Test: All agree after full heal ...
  ... Passed
Test: One peer switches partitions ...
  ... Passed
Test: One peer switches partitions, unreliable ...
  ... Passed
Test: Many requests, changing partitions ...
  ... Passed
PASS
ok  	paxos	53.074s
```

# 总结
有了之前实现Raft的经验，而且这次的实验也只是实现简单的Paxos状态机，相对来说不难实现。
