package pool

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand/v2"
	"sync/atomic"
)

// Sequential 按顺序轮询，用原子计数代替互斥锁
type Sequential struct {
	n atomic.Uint64
}

func (s *Sequential) Key(*Client) Key {
	return Key{Lo: s.n.Add(1) - 1}
}

// Random 每个连接随机选择
type Random struct{}

func (Random) Key(*Client) Key {
	return randomKey()
}

func randomKey() Key {
	return Key{Hi: rand.Uint64(), Lo: rand.Uint64()}
}

// Hash 根据用户名和密码计算 Key，同一组凭据总是得到同一个出口地址
type Hash struct{}

func (Hash) Key(c *Client) Key {
	sum := sha256.Sum256([]byte(c.Username + "\x00" + c.Password))
	return Key{Hi: binary.BigEndian.Uint64(sum[:8]), Lo: binary.BigEndian.Uint64(sum[8:16])}
}
