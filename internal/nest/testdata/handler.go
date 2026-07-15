package testdata

import (
	"github.com/tjbdwanghaibo/cube-core/entity"
	"github.com/tjbdwanghaibo/cube/game/view"
)

var _ view.EntityKind = view.EntityKindPlayer

// IPlayerEntity is a sample entity interface for testing.
type IPlayerEntity interface {
	ID() int64
	Name() string
}

// IAllianceEntity is a sample alliance entity interface.
type IAllianceEntity interface {
	ID() int64
}

type RemoteViewRequest struct {
	TargetPlayerViewRef entity.RemoteViewRef `remote:"view.PlayerViewMapSnapshot,required"`
	LivePlayerViewRef   entity.RemoteViewRef `remote:"read_only,view.PlayerViewMapSnapshot"`
}

// --- Single entity handler ---

//cube:nest
func handlerPlayerLogin(p IPlayerEntity, token string) {
}

// --- Single entity handler with return ---

//cube:nest
func handlerPlayerGetLevel(p IPlayerEntity) (ret int32, err error) {
	return
}

// --- Multi entity handler ---

//cube:nest rollback=dirty
func handlerTransferItem(from IPlayerEntity, to IPlayerEntity, itemId int64, count int32) {
}

// --- Group entity handler ---

//cube:nest
func handlerBroadcastToGroup(targets []IPlayerEntity, sender IPlayerEntity, msg string) {
}

// --- Group handler with return ---

//cube:nest
func handlerGroupCalc(targets []IPlayerEntity, val int64) (ret int64, err error) {
	return
}

// --- Handler with generated remote access ---

//cube:nest
func handlerRemoteView(p IPlayerEntity, req RemoteViewRequest) {
	_, _ = req.TargetPlayer()
	_ = req.MustTargetPlayer()
	_, _ = req.LivePlayer()
}
