package vara

import "sync"

type pubSub struct {
	in        chan connectedState
	req       chan subscriber
	closeOnce *sync.Once
}

type subscriber struct {
	c    chan<- connectedState
	quit <-chan struct{}
}

func (pb pubSub) Close() { pb.closeOnce.Do(func() { close(pb.in) }) }

func newPubSub() pubSub {
	pb := pubSub{
		in:        make(chan connectedState),
		req:       make(chan subscriber),
		closeOnce: new(sync.Once),
	}
	go func() {
		var subscribers []subscriber
		defer func() {
			// Signal all subscribers that the publisher is done.
			for _, r := range subscribers {
				close(r.c)
			}
		}()
		for {
			select {
			case v, ok := <-pb.in:
				if !ok {
					return
				}
				for i := 0; i < len(subscribers); i++ {
					s := subscribers[i]
					select {
					case <-s.quit:
						subscribers = append(subscribers[:i], subscribers[i+1:]...)
						i--
					case s.c <- v:
					}
				}
			case req, ok := <-pb.req:
				if !ok {
					return
				}
				subscribers = append(subscribers, req)
			}
		}
	}()
	return pb
}

func (in pubSub) Publish(v connectedState) {
	in.in <- v
}

func (in pubSub) Subscribe() (<-chan connectedState, func()) {
	c := make(chan connectedState, 1)
	q := make(chan struct{}, 1)
	in.req <- subscriber{c, q}
	return c, func() {
		select {
		case q <- struct{}{}:
		default:
		}
	}
}
