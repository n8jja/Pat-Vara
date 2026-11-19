package vara

import (
	"strings"
	"sync"
)

type pubSub struct {
	in        chan string
	req       chan subscriber
	done      chan struct{} // closed by Close
	closeOnce *sync.Once
}

type subscriber struct {
	c        chan<- string
	quit     <-chan struct{}
	prefixes []string
}

func (s subscriber) wants(v string) bool {
	if s.prefixes == nil {
		return true
	}
	for _, prefix := range s.prefixes {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	return false
}

// Close stops the publisher. Publish and Subscribe may still be called
// afterwards (cmdListen can publish a line it read while the modem closes):
// Publish drops the value and Subscribe returns a closed channel.
func (pb pubSub) Close() { pb.closeOnce.Do(func() { close(pb.done) }) }

func newPubSub() pubSub {
	pb := pubSub{
		in:        make(chan string),
		req:       make(chan subscriber),
		done:      make(chan struct{}),
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
			case <-pb.done:
				return
			case v := <-pb.in:
				for i := 0; i < len(subscribers); i++ {
					s := subscribers[i]
					if !s.wants(v) {
						continue
					}
					select {
					case <-s.quit:
						subscribers = append(subscribers[:i], subscribers[i+1:]...)
						i--
					case s.c <- v:
					case <-pb.done:
						return
					}
				}
			case req := <-pb.req:
				subscribers = append(subscribers, req)
			}
		}
	}()
	return pb
}

func (in pubSub) Publish(v string) {
	select {
	case in.in <- v:
	case <-in.done:
	}
}

func (in pubSub) Subscribe(prefix ...string) (<-chan string, func()) {
	c := make(chan string, 1)
	q := make(chan struct{}, 1)
	select {
	case in.req <- subscriber{c, q, prefix}:
	case <-in.done:
		close(c)
	}
	return c, func() {
		select {
		case q <- struct{}{}:
		default:
		}
	}
}
