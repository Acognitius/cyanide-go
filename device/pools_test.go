/* SPDX-License-Identifier: MIT
 *
  * Copyright (C) 2017-2023 WireGuard LLC. All Rights Reserved.
  * Copyright (C) 2023 Synthesis Labs. All Rights Reserved.
 */

package device

import (
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitPool(t *testing.T) {
	t.Skip("Currently disabled")
	var cn sync.WaitGroup
	var trials atomic.Int32
	startTrials := int32(100000)
	if raceEnabled {
		// This test can be very slow with -race.
		startTrials /= 10
	}
	trials.Store(startTrials)
	workers := runtime.NumCPU() + 2
	if workers-4 <= 0 {
		t.Skip("Not enough cores")
	}
	p := NewWaitPool(uint32(workers-4), func() any { return make([]byte, 16) })
	cn.Add(workers)
	var max atomic.Uint32
	updateMax := func() {
		count := p.count.Load()
		if count > p.max {
			t.Errorf("count (%d) > max (%d)", count, p.max)
		}
		for {
			old := max.Load()
			if count <= old {
				break
			}
			if max.CompareAndSwap(old, count) {
				break
			}
		}
	}
	for i := 0; i < workers; i++ {
		go func() {
			defer cn.Done()
			for trials.Add(-1) > 0 {
				updateMax()
				x := p.Get()
				updateMax()
				time.Sleep(time.Duration(rand.Intn(100)) * time.Microsecond)
				updateMax()
				p.Put(x)
				updateMax()
			}
		}()
	}
	cn.Wait()
	if max.Load() != p.max {
		t.Errorf("Actual maximum count (%d) != ideal maximum count (%d)", max, p.max)
	}
}

func BenchmarkWaitPool(b *testing.B) {
	var cn sync.WaitGroup
	var trials atomic.Int32
	trials.Store(int32(b.N))
	workers := runtime.NumCPU() + 2
	if workers-4 <= 0 {
		b.Skip("Not enough cores")
	}
	p := NewWaitPool(uint32(workers-4), func() any { return make([]byte, 16) })
	cn.Add(workers)
	b.ResetTimer()
	for i := 0; i < workers; i++ {
		go func() {
			defer cn.Done()
			for trials.Add(-1) > 0 {
				x := p.Get()
				time.Sleep(time.Duration(rand.Intn(100)) * time.Microsecond)
				p.Put(x)
			}
		}()
	}
	cn.Wait()
}

func BenchmarkWaitPoolEmpty(b *testing.B) {
	var cn sync.WaitGroup
	var trials atomic.Int32
	trials.Store(int32(b.N))
	workers := runtime.NumCPU() + 2
	if workers-4 <= 0 {
		b.Skip("Not enough cores")
	}
	p := NewWaitPool(0, func() any { return make([]byte, 16) })
	cn.Add(workers)
	b.ResetTimer()
	for i := 0; i < workers; i++ {
		go func() {
			defer cn.Done()
			for trials.Add(-1) > 0 {
				x := p.Get()
				time.Sleep(time.Duration(rand.Intn(100)) * time.Microsecond)
				p.Put(x)
			}
		}()
	}
	cn.Wait()
}

func BenchmarkSyncPool(b *testing.B) {
	var cn sync.WaitGroup
	var trials atomic.Int32
	trials.Store(int32(b.N))
	workers := runtime.NumCPU() + 2
	if workers-4 <= 0 {
		b.Skip("Not enough cores")
	}
	p := sync.Pool{New: func() any { return make([]byte, 16) }}
	cn.Add(workers)
	b.ResetTimer()
	for i := 0; i < workers; i++ {
		go func() {
			defer cn.Done()
			for trials.Add(-1) > 0 {
				x := p.Get()
				time.Sleep(time.Duration(rand.Intn(100)) * time.Microsecond)
				p.Put(x)
			}
		}()
	}
	cn.Wait()
}
