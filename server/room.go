package server

import (
	"sync"
)

type NewAdapterFunc func(room string) Adapter

type adapterCounter struct {
	count   uint64
	adapter Adapter
}

type AdapterRoomManager struct {
	rooms      map[string]*adapterCounter
	roomsMu    sync.RWMutex
	newAdapter NewAdapterFunc
	log        Logger
}

func NewAdapterRoomManager(newAdapter NewAdapterFunc, loggerFactory LoggerFactory) *AdapterRoomManager {
	return &AdapterRoomManager{
		rooms:      map[string]*adapterCounter{},
		newAdapter: newAdapter,
		log:        loggerFactory.GetLogger("memorytracksmanager"),
	}
}

func (r *AdapterRoomManager) Enter(room string) Adapter {
	r.roomsMu.Lock()
	r.log.Printf("Enter -> %s", room)
	adapter, ok := r.rooms[room]
	if ok {
		adapter.count++
	} else {
		adapter = &adapterCounter{
			count:   1,
			adapter: r.newAdapter(room),
		}
		r.rooms[room] = adapter
	}
	r.roomsMu.Unlock()
	return adapter.adapter
}

func (r *AdapterRoomManager) Exit(room string) {
	r.roomsMu.Lock()
	adapter, ok := r.rooms[room]
	if ok {
		adapter.count--
		if adapter.count == 0 {
			delete(r.rooms, room)
			adapter.adapter.Close() // FIXME log error
		}
	}
	r.roomsMu.Unlock()
}
