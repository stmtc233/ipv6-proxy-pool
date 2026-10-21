package pool

import "sync/atomic"

// Sequential 按顺序轮询，用原子计数代替互斥锁
type Sequential struct {
	n atomic.Uint64
}

func (s *Sequential) Key(*Client) uint64 {
	return s.n.Add(1) - 1
}
