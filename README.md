# cube-codegen

Reusable Go code generators for Cube-based projects.

## Commands

Run a released command without adding it to your application's module graph:

```bash
go run github.com/tjbdwanghaibo/cube-codegen/cmd/webroute@v0.1.0 -dir ./service
go run github.com/tjbdwanghaibo/cube-codegen/cmd/attribute@v0.1.0 -dir ./game/gameplay/attribute -force
go run github.com/tjbdwanghaibo/cube-codegen/cmd/errcode@v0.1.0 -root . -out docs/generated/errcode.csv
```

`webroute` generates registrations for `//cube:web` handlers. `attribute`
generates profile metadata from `//cube:attribute` definitions. `errcode`
exports `errcode.Define` declarations as CSV.

The commands preserve the flag names and generated file names used by Cube.
