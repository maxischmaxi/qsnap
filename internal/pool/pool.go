package pool

import "sync"

type Pool struct {
	wg  sync.WaitGroup
	sem chan struct{}
}

func New(concurrency int) *Pool {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Pool{
		sem: make(chan struct{}, concurrency),
	}
}

func (p *Pool) Go(fn func()) {
	p.wg.Add(1)
	p.sem <- struct{}{}
	go func() {
		defer p.wg.Done()
		defer func() { <-p.sem }()
		fn()
	}()
}

func (p *Pool) Wait() {
	p.wg.Wait()
}
