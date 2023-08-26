# Context包
context 包的核心 API 有四个：
- context.WithValue：设置键值对，并且返回一个新的 context 实例
- context.WithCancel
- context.WithDeadline
- context.WithTimeout   
后三者都返回一个 可取消的 context实例 和 取消函数   
注意：context 实例是不可变的，每一次都是新创建的。   

context 的实例之间存在父子关系：   
- 当父亲取消或者超时，所有派生的子 context 都被取消或者超时
- 当找 key 的时候，子 context 先看自己有没有，没有则去祖先里面找   
**控制是从上至下的，查找是从下至上的。**
  
## context作用一：安全传递数据   
是指在请求执行上下文中线程安全地传递数据，依赖于WithValue   
   
例子：
- 链路追踪的 trace id
- AB测试的标记位
- 压力测试标记位
- 分库分表中间件中传递 sharding hint
- ORM 中间件传递 SQL hint
- Web 框架传递上下文   

## context作用二：控制链路   
context 包提供了三个控制方法： WithCancel、WithDeadline 和 WithTimeout。   
三者用法大同小异：    
- 没有过期时间，但是又需要在必要的时候取 消，使用 WithCancel
- 在固定时间点过期，使用 WithDeadline
- 在一段时间后过期，使用 WithTimeout   

而后便是监听 Done() 返回的 channel，不管是主动调用 cancel() 还是超时，都能从这个 channel 里面取出来数据。后面可以用 Err() 方 法来判断究竟是哪种情况。

## xxxCtx的实现   
### 一、valueCtx
```)go
type valueCtx struct {
    Context
    key, val any
}
```
valueCtx 用于存储 key-value 数据，特点：  
- 典型的装饰器模式：在已有 Context 的基础上附加一个存储 key-value 的功能
- 只能存储一个 key, val，其中key须为可比较类型 ---(context包的设计理念就是将Context设计为不可变)   

Go语言中Comparable类型：
在Go语言中，“comparable”类型是指可以进行相等性比较的类型，这意味着你可以使用“==”或“！=”操作符来比较这些类型的值。

*可比较的：*   
- 数值类型（整数类型、浮点数类型等）：
int,int8,int16,int32,int64,uint,uint8,uint16,uint32,uint64,float32,float64,complex64, complex128
- 布尔类型：bool
- 字符串类型：string
- 数组类型
- 指针类型：*T，其中T是一个可比较的类型
- 结构体类型：如果结构体的所有字段都是可比较的，那该结构体也是可比较的
- 接口类型：如果接口中的基础类型是可比较的，那该接口类型也是可以比较的 
<br>

 *不可比较：*
- 切片类型：包含了引用语义，而不是值语义
- 映射类型map: 包含了引用语义，而不是值语义
- 函数类型   

### 二、cancelCtx
```)go
type cancelCtx struct {
	Context

	mu       sync.Mutex            // protects following fields
	done     atomic.Value          // of chan struct{}, created lazily, closed by first cancel call
	children map[canceler]struct{} // set to nil by the first cancel call
	err      error                 // set to non-nil by the first cancel call
	cause    error                 // set to non-nil by the first cancel call
}
```   
特点：   
- 装饰器模式：在已有的Context的基础上，附加上取消的功能
- Done方法是通过类似一个double-check的机制写的。原子操作atomic.Value和锁sync.Mutex结合
    >ps：为什么不用读写锁？   
  > 保证在并发环境下正确、高效的处理取消操作，同时避免不必要的开销：   
    > - 性能：原子操作结合锁，通常能够在性能上更好的满足取消操作的需求。读写锁会引入更多的开销，因为它需要维护读和写的互斥访问。在取消操作频繁的情况下，读写锁可能会导致性能下降 
    > - 原子性：取消操作需要保证在所有相关操作中是原子的。原子操作能够在一次操作中完成读取和写入，而不会中断，确保了取消状态的一致性。 
    > - 简化代码：使用原子操作可以简化代码逻辑，减少错误的可能性。读写锁的使用可能需要更多的代码，包括读锁和写锁的获取和释放，容易出错 
    > - 内存模型：Go语言的内存模型会影响多线程并发访问共享数据时的可见性。原子操作提供了对数据在多线程间的同步和可见性的保证，可以更容易的满足内存模型的要求。
- 利用 children 来维护了所有的衍生节点,实现父子关联 
    >children：核心是儿子把自己加进去父亲的 children 字段里面   
  > 但是因为 Context 里面存在非常多的层级，所以父亲不一定是 cancelCtx，因此本质上是找最近属于 cancelCtx 类型的祖先，然后儿子把自己加进去。   
  > 
  > cancel 就是遍历 children，挨个调用cancel。然后儿子调用孙子的 cancel，子子孙孙无穷匮也。   
  > 
  > 核心的 cancel 方法，做了两件事： 
  > - 遍历所有的 children ,挨个调用 cancel  
  > - 关闭 done 这个 channel：这个符合谁创建谁关闭的原则

### 三、timerCtx
```)go
type timerCtx struct {
	*cancelCtx
	timer *time.Timer // Under cancelCtx.mu.

	deadline time.Time
}
```   
特点：
- 装饰器模式：在已有的Context的基础上，附加上超时的功能
-  WithTimeout和WithDeadline本质一样。WithDeadline里面，在创建timerCtx的时候利用time.AfterFunc来实现超时。
- cancel 方法做的事：
  - 调用 cancelCtx 的 cancel 方法
  - 停掉定时器
