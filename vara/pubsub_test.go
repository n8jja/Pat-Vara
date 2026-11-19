package vara

import (
	"testing"
	"time"
)

// cmdListen can publish a line it has already read while Modem.Close runs.
// That used to panic with "send on closed channel".
func TestPubSubPublishAfterClose(t *testing.T) {
	pb := newPubSub()
	pb.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pb.Publish("DISCONNECTED")
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked after Close")
	}
}

// A Subscribe after Close must not block forever; its channel is closed.
func TestPubSubSubscribeAfterClose(t *testing.T) {
	pb := newPubSub()
	pb.Close()
	got := make(chan bool, 1)
	go func() {
		c, cancel := pb.Subscribe("VERSION")
		defer cancel()
		_, ok := <-c
		got <- ok
	}()
	select {
	case ok := <-got:
		if ok {
			t.Fatal("Subscribe after Close delivered a value")
		}
	case <-time.After(time.Second):
		t.Fatal("Subscribe blocked after Close")
	}
}

// Subscribers still get what is published before Close, and their channel
// is closed by Close.
func TestPubSubDeliverThenClose(t *testing.T) {
	pb := newPubSub()
	c, cancel := pb.Subscribe("BUFFER")
	defer cancel()
	go pb.Publish("BUFFER 12")
	select {
	case v := <-c:
		if v != "BUFFER 12" {
			t.Fatalf("got %q", v)
		}
	case <-time.After(time.Second):
		t.Fatal("no delivery")
	}
	pb.Close()
	select {
	case _, ok := <-c:
		if ok {
			t.Fatal("value after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber channel not closed by Close")
	}
}
