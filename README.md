# roost-codegen

Roost 项目生成器和通用 Go 代码生成器。

## 创建项目

    go run github.com/tjbdwanghaibo/roost-codegen/cmd/project@v1.1.0 planet \
      -services game,gate \
      -mods configdata,etcd,redis,mongo,nats,sync,remote_entity

也可以使用统一入口：

    go run github.com/tjbdwanghaibo/roost-codegen/cmd/roost@v1.1.0 \
      project new planet

生成项目使用 roost.yaml 描述 Service、Kit Mod、生成能力、版本和 ID 空间。

## 统一命令

    roost project new <name>
    roost project sync
    roost project diff
    roost project doctor
    roost project upgrade
    roost generate
    roost generate --changed
    roost generate --check
    roost add service|mod|module|protocol|entity|component|event|table|dao|webroute|errcode
    roost config check
    roost id next|check

完整说明见 [项目生成器使用说明](docs/PROJECT_GENERATOR.zh-CN.md)。

## 独立生成器

所有命令会从目标目录自动发现 Go module：

    go run github.com/tjbdwanghaibo/roost-codegen/cmd/protocol@v1.1.0 -def ./protocol/def -force
    go run github.com/tjbdwanghaibo/roost-codegen/cmd/webroute@v1.1.0 -dir ./service
    go run github.com/tjbdwanghaibo/roost-codegen/cmd/attribute@v1.1.0 -dir ./game/gameplay/attribute -force
    go run github.com/tjbdwanghaibo/roost-codegen/cmd/dao@v1.1.0 -def ./db/def -out ./db
    go run github.com/tjbdwanghaibo/roost-codegen/cmd/entity@v1.1.0 -dir ./game
    go run github.com/tjbdwanghaibo/roost-codegen/cmd/errcode@v1.1.0 -root . -out docs/generated/errcode.csv
    go run github.com/tjbdwanghaibo/roost-codegen/cmd/eventgen@v1.1.0 -def ./event/def -out ./event -game ./game
    go run github.com/tjbdwanghaibo/roost-codegen/cmd/nest@v1.1.0 -dir ./game
    go run github.com/tjbdwanghaibo/roost-codegen/cmd/tablegen@v1.1.0 -meta ./configs/schema -out ./configs/generated

旧的 //cube:* 标记继续兼容，避免已有项目迁移时批量修改业务定义。
