package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Evaluator interface {
	Eval(any, any) error
	Close()
}
type CDP struct {
	ws  *websocket.Conn
	seq int
	mu  sync.Mutex
}
type target struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	WS    string `json:"webSocketDebuggerUrl"`
}

func connect(port int) (*CDP, error) {
	client := &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json", port))
	if err != nil {
		return nil, fmt.Errorf("无法连接 Steam，请先用菜单 1 启动连接模式：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("连接端口不是可用的 Steam 调试端点 (HTTP %d)", resp.StatusCode)
	}
	var ts []target
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&ts); err != nil {
		return nil, err
	}
	for _, title := range []string{"SharedJSContext", "Steam Shared Context presented by Valve™", "Steam", "SP"} {
		for _, t := range ts {
			u, e := url.Parse(t.URL)
			if e != nil || t.Title != title || u.Scheme != "https" || u.Hostname() != "steamloopback.host" || !(strings.HasPrefix(u.Path, "/routes/") || u.Path == "/index.html") {
				continue
			}
			w, e := url.Parse(t.WS)
			if e != nil || w.Scheme != "ws" || (w.Hostname() != "127.0.0.1" && w.Hostname() != "localhost" && w.Hostname() != "::1") || w.Port() != fmt.Sprint(port) {
				continue
			}
			dial := websocket.Dialer{HandshakeTimeout: 5 * time.Second, Proxy: nil}
			ws, _, e := dial.Dial(t.WS, nil)
			if e != nil {
				return nil, e
			}
			ws.SetReadLimit(64 << 20)
			return &CDP{ws: ws}, nil
		}
	}
	return nil, fmt.Errorf("Steam 的库上下文未就绪。请打开库，等待载入后重试；不要同时运行其他收藏管理器")
}
func (c *CDP) Close() {
	if c.ws != nil {
		c.ws.Close()
	}
}
func (c *CDP) Eval(arg any, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	b, err := json.Marshal(arg)
	if err != nil {
		return err
	}
	expression := "(async()=>{try{return {ok:true,value:await (" + bridgeSource + ")(" + string(b) + ")};}catch(e){return {ok:false,error:String(e&&e.message||e)};}})()"
	req := map[string]any{"id": c.seq, "method": "Runtime.evaluate", "params": map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true, "allowUnsafeEvalBlockedByCSP": true}}
	c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err = c.ws.WriteJSON(req); err != nil {
		c.Close()
		return err
	}
	c.ws.SetReadDeadline(time.Now().Add(45 * time.Second))
	for {
		var msg struct {
			ID     int             `json:"id"`
			Error  json.RawMessage `json:"error"`
			Result struct {
				Result struct {
					Value struct {
						OK    bool            `json:"ok"`
						Value json.RawMessage `json:"value"`
						Error string          `json:"error"`
					} `json:"value"`
				} `json:"result"`
				Exception json.RawMessage `json:"exceptionDetails"`
			} `json:"result"`
		}
		if err = c.ws.ReadJSON(&msg); err != nil {
			c.Close()
			return fmt.Errorf("连接中断或操作超时。停止后续操作，重启 Steam 后重新预览；不要盲目重试：%w", err)
		}
		if msg.ID != c.seq {
			continue
		}
		if len(msg.Error) > 0 || len(msg.Result.Exception) > 0 {
			return fmt.Errorf("Steam 调试接口返回异常：%s %s", msg.Error, msg.Result.Exception)
		}
		v := msg.Result.Result.Value
		if !v.OK {
			return fmt.Errorf("Steam：%s", v.Error)
		}
		if out != nil {
			return json.Unmarshal(v.Value, out)
		}
		return nil
	}
}
