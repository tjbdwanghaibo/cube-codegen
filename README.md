# cube-codegen

Reusable Go code generators for Cube-based projects. Commands discover the
target Go module from their directory arguments and generate same-module
imports from source and directory metadata.

## Commands

Run a released command without adding it to your application's module graph:

```bash
go run github.com/tjbdwanghaibo/cube-codegen/cmd/webroute@v0.1.0 -dir ./service
go run github.com/tjbdwanghaibo/cube-codegen/cmd/attribute@v0.1.0 -dir ./game/gameplay/attribute -force
go run github.com/tjbdwanghaibo/cube-codegen/cmd/dao@v0.1.0 -def ./db/def -out ./db
go run github.com/tjbdwanghaibo/cube-codegen/cmd/entity@v0.1.0 -dir ./game
go run github.com/tjbdwanghaibo/cube-codegen/cmd/errcode@v0.1.0 -root . -out docs/generated/errcode.csv
go run github.com/tjbdwanghaibo/cube-codegen/cmd/eventgen@v0.1.0 -def ./event/def -out ./event -game ./game
go run github.com/tjbdwanghaibo/cube-codegen/cmd/nest@v0.1.0 -dir ./game
go run github.com/tjbdwanghaibo/cube-codegen/cmd/tablegen@v0.1.0 -meta ./configs/schema -out ./configs/generated
```

`webroute` generates registrations for `//cube:web` handlers. `attribute`
generates profile metadata from `//cube:attribute` definitions. `dao`, `entity`,
`eventgen`, `nest`, and `tablegen` generate their respective persistence,
entity, event, dispatch, and table artifacts. `errcode` exports
`errcode.Define` declarations as CSV.

The commands preserve the flag names and generated file names used by Cube.
