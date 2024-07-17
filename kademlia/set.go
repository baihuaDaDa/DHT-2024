package kademlia

import (
	"container/list"
	"math/big"
	"sync"
)

type SetUnit struct {
	addr string
	dist *big.Int
}

func (unit *SetUnit) Cmp(tar SetUnit) int {
	return unit.dist.Cmp(tar.dist)
}

type Set struct {
	sourceId *big.Int
	list     *list.List
	called   map[string]bool
	visited  map[string]bool
	setLock  sync.RWMutex
}

func (set *Set) init(src *big.Int) {
	set.setLock.Lock()
	defer set.setLock.Unlock()
	set.sourceId = src
	set.list = list.New()
	set.called = make(map[string]bool)
	set.visited = make(map[string]bool)
}

func (set *Set) mark(addr string) {
	set.setLock.Lock()
	defer set.setLock.Unlock()
	set.visited[addr] = true
	set.called[addr] = true
}

func (set *Set) empty() bool {
	set.setLock.RLock()
	defer set.setLock.RUnlock()
	return set.list.Len() == 0
}

func (set *Set) insert(addr string) bool {
	set.setLock.Lock()
	defer set.setLock.Unlock()
	if set.visited[addr] {
		return false
	}
	set.visited[addr] = true
	newUnit := SetUnit{addr, set.sourceId.Xor(set.sourceId, Hash(addr))}
	for unit := set.list.Front(); unit != nil; unit = unit.Next() {
		if newUnit.Cmp(unit.Value.(SetUnit)) < 0 {
			set.list.InsertBefore(newUnit, unit)
			return true
		}
	}
	set.list.PushBack(newUnit)
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
	if set.list.Front() == nil {
		return ""
	}
	return set.list.Front().Value.(SetUnit).addr
}

func (set *Set) getCallList() (callList []string) {
	set.setLock.RLock()
	defer set.setLock.RUnlock()
	for unit := set.list.Front(); unit != nil; unit = unit.Next() {
		addr := unit.Value.(SetUnit).addr
		if !set.called[addr] {
			callList = append(callList, addr)
			set.called[addr] = true
		}
		if len(callList) == alpha {
			return callList
		}
	}
	return callList
}

func (set *Set) getNodeList() (nodeList []string) {
	set.setLock.RLock()
	defer set.setLock.RUnlock()
	for unit := set.list.Front(); unit != nil; unit = unit.Next() {
		addr := unit.Value.(SetUnit).addr
		nodeList = append(nodeList, addr)
		if len(nodeList) == k {
			return nodeList
		}
	}
	return nodeList
}
