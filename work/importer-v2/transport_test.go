package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestTransportSerialAndSteamTargetGuard(t *testing.T) {
	var srv *httptest.Server
	var mu sync.Mutex
	calls := 0
	up := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[{"title":"SharedJSContext","url":"https://steamloopback.host/routes/library","webSocketDebuggerUrl":%q}]`, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, e := up.Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer c.Close()
		for {
			var q struct {
				ID     int            `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if c.ReadJSON(&q) != nil {
				return
			}
			if q.Method != "Runtime.evaluate" || q.Params["awaitPromise"] != true {
				t.Error("wrong protocol")
			}
			mu.Lock()
			calls++
			mu.Unlock()
			c.WriteJSON(map[string]any{"id": q.ID, "result": map[string]any{"result": map[string]any{"value": map[string]any{"ok": true, "value": map[string]int{"n": q.ID}}}}})
		}
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	_, p, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	port, _ := strconv.Atoi(p)
	c, e := connect(port)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out map[string]int
			if e := c.Eval(map[string]string{"op": "snapshot"}, &out); e != nil || out["n"] == 0 {
				t.Error(e, out)
			}
		}()
	}
	wg.Wait()
	if calls != 3 {
		t.Fatal(calls)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"title":"SharedJSContext","url":"https://example.com/routes/library","webSocketDebuggerUrl":"ws://example.com:8080/ws"}]`)
	}))
	defer bad.Close()
	_, p, _ = net.SplitHostPort(strings.TrimPrefix(bad.URL, "http://"))
	port, _ = strconv.Atoi(p)
	if _, e = connect(port); e == nil {
		t.Fatal("non-Steam remote target accepted")
	}
}
