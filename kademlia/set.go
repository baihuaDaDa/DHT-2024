package kademlia

import (
	"container/list"
	"math/big"
	"sync"
)

type SetUnit struct {
	addr string
	id   *big.Int
}

func (unit *SetUnit) Cmp(tar SetUnit) int {
	return unit.id.Cmp(tar.id)
}

type Set struct {
	list    *list.List
	visited map[string]bool
	setLock sync.RWMutex
}

func (set *Set) init() {
	set.list = list.New()
	set.visited = make(map[string]bool)
}

func (set *Set) insert(addr string) bool {
	set.setLock.Lock()
	defer set.setLock.Unlock()
	if set.visited[addr] {
		return false
	}
	newUnit := SetUnit{addr, Hash(addr)}
	for unit := set.list.Front(); unit != nil; unit = unit.Next() {
		if newUnit.Cmp(unit.Value.(SetUnit)) < 0 {
			set.list.InsertBefore(newUnit, unit)
			return true
		}
	}
	set.list.InsertBefore(newUnit, nil)
	return true
}

func (set *Set) delete(addr string) bool {
	set.setLock.Lock()
	defer set.setLock.Unlock()
	for unit := set.list.Front(); unit != nil; unit = unit.Next() {
		if unit.Value.(SetUnit).addr == addr {
			set.list.Remove(unit)
			return true
		}
	}
	return false
}

func (set *Set) getFront() string {
	set.setLock.RLock()
	defer set.setLock.RUnlock()
	return set.list.Front().Value.(SetUnit).addr
}

func (set *Set) getCallList() (callList []string) {
	set.setLock.RLock()
	defer set.setLock.RUnlock()
	for unit := set.list.Front(); unit != nil; unit = unit.Next() {
		addr := unit.Value.(SetUnit).addr
		if !set.visited[addr] {
			callList = append(callList, addr)
			set.visited[addr] = true
		}
		if len(callList) == alpha {
			return callList
		}
	}
	return callList
}
