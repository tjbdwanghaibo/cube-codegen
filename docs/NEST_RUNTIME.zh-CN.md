# Nest 业务执行模型与生成器

## Handler 定义

新项目使用 `//roost:nest`。`target`/`targets` 明确指出函数开头有多少个
Entity capability 参数，因此 capability 无需以 `Entity` 命名。

```go
type BagOwner interface {
	Bag() BagComponent
}

//roost:nest target=player
func handlerUseItem(owner BagOwner, cmd UseItemCommand) (UseItemResult, error) {
	return owner.Bag().Use(cmd)
}

//roost:nest targets=player,player rollback=undo durability=strict
func handlerTransfer(from BagOwner, to BagOwner, cmd TransferCommand) error {
	return transfer(from.Bag(), to.Bag(), cmd)
}
```

`targets` 以逗号分隔且不包含空格。Entity 参数必须位于普通参数之前。切片
capability（例如 `[]UnitOwner`）生成 MultiGroup 调用。`sync` 可为没有返回值的
handler 强制生成等待完成的同步 Sender。

需要配置、时钟或只读查询依赖时使用指针方法 Handler：

```go
type BagHandler struct {
	configs ConfigReader
	clock   clock.Clock
}

//roost:nest target=player
func (h *BagHandler) handlerUseItem(owner BagOwner, cmd UseItemCommand) error {
	return owner.Bag().Use(h.configs, h.clock, cmd)
}

handler := &BagHandler{configs: configs, clock: clock}
baghandler.RegisterHandlerBagNestHandlers(handler)
```

同一源文件不能混合包函数和不同 receiver 的方法 Handler。方法 Handler 需要实例，
因此不会进入无参 bootstrap；启动层必须显式构造并注册，注册时传入 nil 会立即失败。

旧 `//cube:nest` 和 `rollback=dirty` 不属于生产协议；升级项目应先转换标记再重新生成。

## 注入式 Sender

假设源文件为 `handler_bag.go`，生成器在 sender 包生成 `BagSender`：

```go
asyncSender := bagsender.NewBagSender(nestClient)
syncSender := bagsyncsender.NewBagSender(nestClient)

if err := asyncSender.Send_UseItem(ctx, playerID, cmd); err != nil {
	// queue full、engine stopped、context canceled 必须在接入层处理
}

result, err := syncSender.Sync_UseItem(ctx, playerID, cmd)
```

Sender 只依赖 `nest.Client`，不会读取全局 `nest.Nest`。因此单元测试可以注入
fake Client，生产环境从 `app.Registry` 取得 kit `nest.Mod` 提供的 Client。

## 锁与跨 Entity 规则

- Handler 中所有会被修改的 Entity 必须全部出现在 Entity 参数列表。
- Multi/MultiGroup 由 core 统一排序并加锁；业务不得自行访问 EntityManager。
- Handler 内禁止再次 Dispatch/Request，避免锁顺序反转和死锁。
- 外部 I/O、跨服务 RPC 和消息发布放在锁外，通过 Saga/Outbox 编排。
- Handler 参数和异步命令在成功入队后视为不可变；需要修改的 DTO 应在接入层复制。

## 升级步骤

1. 把宽 `IPlayerEntity` 拆成 handler 所需的窄 capability。
2. 把 marker 改为 `//roost:nest target=player`。
3. 重新运行 `roost generate` 或 `cmd/nest`。
4. 在 endpoint 构造函数中注入生成的 Sender。
5. 将包级 `Send_*`/`Sync_*` 调用改为 Sender 方法；生成器不会保留旧函数。
6. 删除业务 Runtime、Controller 透传层和全局 Access 获取 Nest 的代码。
