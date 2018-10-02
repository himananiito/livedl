package gorman

import (
	"sync"
)

type GoroutineManager struct {
	channels map[chan struct{}] struct{}
	mtxChan sync.Mutex

	mtxWg sync.Mutex
	wg sync.WaitGroup

	codeChecker func(code int)
}

func NewManager() *GoroutineManager {
	return &GoroutineManager{
		channels: map[chan struct{}] struct{}{},
	}
}
func WithChecker(f func(int)) *GoroutineManager {
	return &GoroutineManager{
		channels: map[chan struct{}] struct{}{},
		codeChecker: f,
	}
}
func (gm *GoroutineManager) addChan(c chan struct{}) {
	gm.mtxChan.Lock()
	defer gm.mtxChan.Unlock()
	gm.channels[c] = struct{}{}
}
func (gm *GoroutineManager) delChan(c chan struct{}) {
	gm.mtxChan.Lock()
	defer gm.mtxChan.Unlock()
	delete(gm.channels, c)
}
func (gm *GoroutineManager) Cancel() {
	gm.mtxChan.Lock()
	defer gm.mtxChan.Unlock()
	for c, _ := range gm.channels {
		close(c)
		delete(gm.channels, c)
	}
}
func (gm *GoroutineManager) Count() int {
	gm.mtxChan.Lock()
	defer gm.mtxChan.Unlock()
	return len(gm.channels)
}
func (gm *GoroutineManager) Go(f func(<-chan struct{}) int) {
	gm.wg.Add(1)
	stopChan := make(chan struct{}, 1)
	gm.addChan(stopChan)

	go func(){
		defer gm.wg.Done()
		code := f(stopChan)
		gm.delChan(stopChan)
		if gm.codeChecker != nil {
			gm.codeChecker(code)
		}
	}()
}
func (gm *GoroutineManager) RegisterCodeChecker(f func(int)) {
	gm.codeChecker = f
}
func (gm *GoroutineManager) Wait() {
	gm.wg.Wait()
}
