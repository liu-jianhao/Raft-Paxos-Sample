# Paxos
这次实验目的是要实现Paxos算法，为之后的KV-Paxos做准备。

## 实验框架

```go
px = paxos.Make(peers []string, me int)
px.Start(seq int, v interface{}) // start agreement on new instance
px.Status(seq int) (fate Fate, v interface{}) // get info about an instance
px.Done(seq int) // ok to forget all instances <= seq
px.Max() int // highest instance seq known, or -1
px.Min() int // instances before this have been forgotten
```