package zk

import (
	"context"
	"io/ioutil"
	"sync"
	"testing"
	"time"
)

func TestRecurringReAuthHang(t *testing.T) {
	zkC, err := StartTestCluster(t, 3, ioutil.Discard, ioutil.Discard)
	if err != nil {
		panic(err)
	}
	defer zkC.Stop()

	conn, evtC, err := zkC.ConnectAll()
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	waitForSession(ctx, evtC)
	// Add auth.
	conn.AddAuth("digest", []byte("test:test"))

	var reauthCloseOnce sync.Once
	reauthSig := make(chan struct{}, 1)
	conn.resendZkAuthFn = func(ctx context.Context, c *Conn) error {
		// in current implimentation the reauth might be called more than once based on various conditions
		reauthCloseOnce.Do(func() { close(reauthSig) })
		return resendZkAuth(ctx, c)
	}

	conn.debugCloseRecvLoop = true
	currentServer := conn.Server()
	zkC.StopServer(currentServer)
	// wait connect to new zookeeper.
	ctx, cancel = context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	waitForSession(ctx, evtC)

	select {
	case _, ok := <-reauthSig:
		if !ok {
			return // we closed the channel as expected
		}
		t.Fatal("reauth testing channel should have been closed")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestDeadlockInClose(t *testing.T) {
	c := &Conn{
		shouldQuit:     make(chan struct{}),
		connectTimeout: 1 * time.Second,
		sendChan:       make(chan *request, sendChanSize),
		logger:         DefaultLogger,
	}

	for i := 0; i < sendChanSize; i++ {
		c.sendChan <- &request{}
	}

	okChan := make(chan struct{})
	go func() {
		c.Close()
		close(okChan)
	}()

	select {
	case <-okChan:
	case <-time.After(3 * time.Second):
		t.Fatal("apparent deadlock!")
	}
}

func TestGetEphemerals(t *testing.T) {

	zkC, err := StartTestCluster(t, 3, ioutil.Discard, ioutil.Discard)
	if err != nil {
		panic(err)
	}
	defer zkC.Stop()

	conn, evtC, err := zkC.ConnectAll()
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	waitForSession(ctx, evtC)

	conn.Create("/zktest", []byte("placeholder"), 0, WorldACL(PermAll))
	conn.Create("/zktest/testblabla", []byte("placeholder"), FlagEphemeral, WorldACL(PermAll))
	conn.Create("/zktest/nonono", []byte("placeholder"), FlagEphemeral, WorldACL(PermAll))
	conn.Create("/testE", []byte("placeholder"), FlagEphemeral, WorldACL(PermAll))
	conn.Create("/testE2", []byte("placeholder"), FlagEphemeral, WorldACL(PermAll))

	e1, _ := conn.GetEphemerals("/zktest")
	if len(e1) != 2 {
		t.Fatalf("GetEphemerals /zktest result should be 2, but got %d", len(e1))
	}

	e2, _ := conn.GetEphemerals("/")
	if len(e2) != 4 {
		t.Fatalf("GetEphemerals / result should be 4, but got %d", len(e2))
	}

	e3, _ := conn.GetEphemerals("/testE")
	if len(e3) != 2 {
		t.Fatalf("GetEphemerals /testE result should be 2, but got %d", len(e3))
	}

}