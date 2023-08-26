package quic

import (
	"errors"
)

type Tester[T any] struct {
	Map map[string]T
}

func NewTester[T any]() *Tester[T] {
	return &Tester[T]{Map: make(map[string]T)}
}

func (t *Tester[T]) Add(name string, val T) (err error) {
	t.Map[name] = val
	return
}

func (t *Tester[T]) TestPacket(packet []byte) (bool, string, T) {
	var err error
	var emptyVal T

	sni, err := t.SniffQuic(packet)
	if err != nil && !errors.Is(err, NotFoundError) {
		return false, "", emptyVal
	}

	if val, ok := t.Map[sni]; ok {
		return true, sni, val
	}
	if val, ok := t.Map[""]; ok {
		return true, sni, val
	}

	return false, "", emptyVal
}
