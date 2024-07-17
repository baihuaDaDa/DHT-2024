package kademlia

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type Data struct {
	dataSet       map[string]string
	RepublishTime map[string]time.Time
	ExpireTime    map[string]time.Time
	dataLock      sync.RWMutex
}

const tRepublish time.Duration = 1200 * time.Second
const tExpire time.Duration = 2400 * time.Second

func (data *Data) init() {
	data.dataLock.Lock()
	data.dataSet = make(map[string]string)
	data.RepublishTime = make(map[string]time.Time)
	data.ExpireTime = make(map[string]time.Time)
	data.dataLock.Unlock()
}

func (data *Data) get(key string) (string, bool) {
	data.dataLock.RLock()
	value, ok := data.dataSet[key]
	data.dataLock.RUnlock()
	return value, ok
}

func (data *Data) put(pair Pair) {
	data.dataLock.Lock()
	data.dataSet[pair.Key] = pair.Value
	data.RepublishTime[pair.Key] = time.Now().Add(tRepublish)
	data.ExpireTime[pair.Key] = time.Now().Add(tExpire)
	data.dataLock.Unlock()
}

func (data *Data) getAll() (dataList []Pair) {
	data.dataLock.RLock()
	defer data.dataLock.RUnlock()
	for key, value := range data.dataSet {
		dataList = append(dataList, Pair{key, value})
	}
	return dataList
}

func (data *Data) getRepublishList() (dataList []Pair) {
	data.dataLock.RLock()
	defer data.dataLock.RUnlock()
	for key, republishTime := range data.RepublishTime {
		if time.Now().After(republishTime) {
			dataList = append(dataList, Pair{key, data.dataSet[key]})
		}
	}
	return dataList
}
func (data *Data) expire() {
	var expireList []string
	data.dataLock.RLock()
	for key, expireTime := range data.ExpireTime {
		if time.Now().After(expireTime) {
			expireList = append(expireList, key)
		}
	}
	data.dataLock.RUnlock()
	data.dataLock.Lock()
	for i := range expireList {
		logrus.Infof("expire %s", expireList[i])
		delete(data.dataSet, expireList[i])
		delete(data.ExpireTime, expireList[i])
		delete(data.RepublishTime, expireList[i])
	}
	data.dataLock.Unlock()
}
