package kademlia

import (
	"container/list"
	"sync"
)

type Bucket struct {
	list       *list.List
	node       *Node
	bucketLock sync.RWMutex
}

func (bucket *Bucket) init(node *Node) {
	bucket.bucketLock.Lock()
	defer bucket.bucketLock.Unlock()
	bucket.list = list.New()
	bucket.node = node
}

func (bucket *Bucket) pushBack(addr string) {
	bucket.bucketLock.Lock()
	defer bucket.bucketLock.Unlock()
	bucket.list.PushBack(addr)
}

func (bucket *Bucket) popFront() {
	bucket.bucketLock.Lock()
	defer bucket.bucketLock.Unlock()
	bucket.list.Remove(bucket.list.Front())
}

func (bucket *Bucket) shiftToBack() {
	bucket.bucketLock.Lock()
	defer bucket.bucketLock.Unlock()
	bucket.list.MoveAfter(bucket.list.Front(), bucket.list.Back())
}

func (bucket *Bucket) find(addr string) *list.Element {
	bucket.bucketLock.RLock()
	defer bucket.bucketLock.RUnlock()
	for unit := bucket.list.Front(); unit != nil; unit = unit.Next() {
		if unit.Value.(string) == addr {
			return unit
		}
	}
	return nil
}

func (bucket *Bucket) delete(addr string) bool {
	bucket.bucketLock.Lock()
	defer bucket.bucketLock.Unlock()
	for unit := bucket.list.Front(); unit != nil; unit = unit.Next() {
		if unit.Value.(string) == addr {
			bucket.list.Remove(unit)
			return true
		}
	}
	return false
}

func (bucket *Bucket) front() string {
	bucket.bucketLock.RLock()
	defer bucket.bucketLock.RUnlock()
	return bucket.list.Front().Value.(string)
}

func (bucket *Bucket) size() int {
	bucket.bucketLock.RLock()
	defer bucket.bucketLock.RUnlock()
	return bucket.list.Len()
}

func (bucket *Bucket) flush(addr string, online bool) {
	if online {
		pos := bucket.find(addr)
		if pos != nil {
			bucket.shiftToBack()
		} else {
			if bucket.size() < k {
				bucket.pushBack(addr)
			} else {
				if bucket.node.ping(bucket.front()) {
					bucket.shiftToBack()
				} else {
					bucket.popFront()
					bucket.pushBack(addr)
				}
			}
		}
	} else {
		bucket.delete(addr)
	}
}

func (bucket *Bucket) getAll() (nodeList []string) {
	bucket.bucketLock.RLock()
	defer bucket.bucketLock.Unlock()
	for unit := bucket.list.Front(); unit != nil; unit = unit.Next() {
		nodeList = append(nodeList, unit.Value.(string))
	}
	return nodeList
}
