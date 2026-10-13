// Package claudedesktop uses the owner's explicitly enabled, loopback-only
// Desktop debugger. It calls the same LocalSessions input API as the composer;
// it never impersonates a Remote Control client or changes message provenance.
package claudedesktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var ErrUnavailable = errors.New("Claude Desktop local debugger unavailable")

type Target struct {
	Host, Native, CWD, Project string
}

type Snapshot struct {
	Found    bool   `json:"found"`
	Accepted bool   `json:"accepted"`
	Running  bool   `json:"running"`
	Waiting  bool   `json:"waiting"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
	Fast     bool   `json:"fast"`
}

type Image struct {
	Base64 string `json:"base64"`
	MIME   string `json:"mimeType"`
	Name   string `json:"filename,omitempty"`
}

type Permission struct {
	ID          string         `json:"requestId"`
	Session     string         `json:"sessionId"`
	Tool        string         `json:"toolName"`
	Input       map[string]any `json:"input"`
	Description string         `json:"description,omitempty"`
	Reason      string         `json:"decisionReason,omitempty"`
}

type State struct {
	Running     bool         `json:"running"`
	Permissions []Permission `json:"permissions"`
}

type Bridge struct {
	conn *websocket.Conn
	id   int
	wc   int
	pid  int
}

// Open deliberately has no configurable host, proxy, inspector expression or
// renderer URL. None of these capabilities are exposed in the phone protocol.
func Open(ctx context.Context) (*Bridge, error) {
	identity, err := debuggerIdentity(ctx)
	if err != nil {
		return nil, ErrUnavailable
	}
	httpClient := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable }}
	defer httpClient.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:9229/json", nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	var targets []struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&targets) != nil || len(targets) != 1 || !validDebuggerURL(targets[0].WS) {
		return nil, ErrUnavailable
	}
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second, Proxy: nil}
	conn, _, err := dialer.DialContext(ctx, targets[0].WS, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	b := &Bridge{conn: conn}
	conn.SetReadLimit(1 << 20)
	latest, err := debuggerIdentity(ctx)
	if err != nil || latest != identity {
		b.Close()
		return nil, ErrUnavailable
	}
	var pid int
	if err := b.evaluate(ctx, "process.pid", &pid); err != nil || pid != identity.PID {
		b.Close()
		return nil, ErrUnavailable
	}
	b.pid = pid
	return b, nil
}

// ShutdownDebugger is only used by a Local instance explicitly configured to
// close the owner's debugger on shutdown. It never enables a debugger.
func (b *Bridge) ShutdownDebugger(ctx context.Context) error {
	if b.pid <= 0 {
		return ErrUnavailable
	}
	identity, err := debuggerIdentity(ctx)
	if err != nil || identity.PID != b.pid {
		return ErrUnavailable
	}
	var ok bool
	err = b.evaluate(ctx, `(function(){if(process.pid!==`+strconv.Itoa(b.pid)+`)throw Error('wrong process');setTimeout(()=>process.mainModule.require('inspector').close(),100);return true})()`, &ok)
	if err != nil || !ok {
		return ErrUnavailable
	}
	return nil
}

func validDebuggerURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "ws" && u.Host == "127.0.0.1:9229" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && len(u.Path) > 1 && !strings.Contains(u.Path, "..")
}

func (b *Bridge) Close() { _ = b.conn.Close() }

func (b *Bridge) evaluate(ctx context.Context, expression string, out any) error {
	deadline := time.Now().Add(6 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	b.id++
	_ = b.conn.SetWriteDeadline(deadline)
	if err := b.conn.WriteJSON(map[string]any{"id": b.id, "method": "Runtime.evaluate", "params": map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}}); err != nil {
		return err
	}
	_ = b.conn.SetReadDeadline(deadline)
	for {
		var frame struct {
			ID     int             `json:"id"`
			Error  json.RawMessage `json:"error"`
			Result struct {
				Exception json.RawMessage `json:"exceptionDetails"`
				Result    struct {
					Value json.RawMessage `json:"value"`
				} `json:"result"`
			} `json:"result"`
		}
		if err := b.conn.ReadJSON(&frame); err != nil {
			return err
		}
		if frame.ID != b.id {
			continue
		}
		// Exceptions may contain sensitive session/configuration data. Never
		// propagate their text into phone errors, logs or acceptance reports.
		if len(frame.Error) != 0 || len(frame.Result.Exception) != 0 {
			return errors.New("Claude Desktop native API failed")
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(frame.Result.Result.Value, out)
	}
}

func js(v any) string { data, _ := json.Marshal(v); return string(data) }

func (b *Bridge) renderer(ctx context.Context, code string, out any) error {
	if b.wc == 0 {
		var candidates []int
		if err := b.evaluate(ctx, `(function(){const {webContents}=process.mainModule.require('electron');return webContents.getAllWebContents().filter(w=>{try{const u=new URL(w.getURL());return u.protocol==='https:'&&u.hostname==='claude.ai'&&u.pathname.startsWith('/epitaxy')}catch{return false}}).map(w=>w.id)})()`, &candidates); err != nil || len(candidates) != 1 {
			return ErrUnavailable
		}
		b.wc = candidates[0]
	}
	expr := `(async()=>{const w=process.mainModule.require('electron').webContents.fromId(` + strconv.Itoa(b.wc) + `);if(!w||w.isDestroyed())throw Error('closed');const u=new URL(w.getURL());if(u.protocol!=='https:'||u.hostname!=='claude.ai'||!u.pathname.startsWith('/epitaxy'))throw Error('wrong renderer');return await w.executeJavaScript(` + js(code) + `)})()`
	return b.evaluate(ctx, expr, out)
}

func sessionCheck(t Target) string {
	return `const l=window['claude.web']?.LocalSessions;if(!l||typeof l.sendMessage!=='function')throw Error('missing bridge');const s=await l.getSession(` + js(t.Host) + `);if(!s||s.sessionId!==` + js(t.Host) + `||s.cwd!==` + js(t.CWD) + `||(s.originCwd||s.cwd)!==` + js(t.Project) + `||s.isArchived||s.remoteTarget)throw Error('wrong session');`
}

func (b *Bridge) Snapshot(ctx context.Context, t Target, uuid string) (Snapshot, error) {
	var result Snapshot
	code := `(async()=>{` + sessionCheck(t) + `const tail=await l.getTranscriptTail(s.sessionId,128);const row=tail.messages?.find(m=>m.uuid===` + js(uuid) + `);return {found:true,accepted:!!(row&&row.type==='user'&&row.sessionId===` + js(t.Native) + `&&!row.isSidechain&&row.origin?.kind==='human'&&row.turnOrigin==='human'),running:!!s.turnRunning,waiting:!!s.pendingToolPermissions?.length,model:s.model||'',effort:s.effort||'',fast:s.fastModeState==='on'}})()`
	err := b.renderer(ctx, code, &result)
	return result, err
}

// Send only invokes the normal owner's input handler. The caller must persist
// a dispatch receipt before calling; an RPC acknowledgement alone is not proof
// of transcript acceptance. A late/failed reply must never trigger a fallback.
// Desktop's promise may remain pending for the turn. Do not await it inside the
// inspector RPC: that can time out and poison the socket after input has landed.
// Dispatch once, then let Snapshot independently prove exact human acceptance.
func nativeSendCode(t Target, text, uuid string) string {
	return nativeImageSendCode(t, text, uuid, nil)
}

func nativeImageSendCode(t Target, text, uuid string, images []Image) string {
	encoded := "undefined"
	if len(images) > 0 {
		encoded = js(images)
	}
	return `(async()=>{` + sessionCheck(t) + `const pending=l.sendMessage(s.sessionId,` + js(text) + `,` + encoded + `,undefined,undefined,undefined,undefined,` + js(uuid) + `);Promise.resolve(pending).catch(()=>{});return true})()`
}

func (b *Bridge) Send(ctx context.Context, t Target, text, uuid string) error {
	return b.SendImages(ctx, t, text, uuid, nil)
}

func (b *Bridge) SendImages(ctx context.Context, t Target, text, uuid string, images []Image) error {
	var called bool
	if err := b.renderer(ctx, nativeImageSendCode(t, text, uuid, images), &called); err != nil {
		return err
	}
	if !called {
		return fmt.Errorf("Claude Desktop native API returned no acknowledgement")
	}
	return nil
}

func (b *Bridge) State(ctx context.Context, t Target) (State, error) {
	var out State
	code := `(async()=>{` + sessionCheck(t) + `return {running:!!s.turnRunning,permissions:(s.pendingToolPermissions||[]).filter(p=>p.sessionId===s.sessionId).map(p=>({requestId:p.requestId,sessionId:p.sessionId,toolName:p.toolName,input:p.input,description:p.description,decisionReason:p.decisionReason}))}})()`
	err := b.renderer(ctx, code, &out)
	return out, err
}

func (b *Bridge) Interrupt(ctx context.Context, t Target) error {
	var ok bool
	err := b.renderer(ctx, `(async()=>{`+sessionCheck(t)+`if(typeof l.interrupt!=='function')throw Error('unsupported');await l.interrupt(s.sessionId);return true})()`, &ok)
	if err == nil && !ok {
		return errors.New("Claude Desktop interrupt not acknowledged")
	}
	return err
}

func (b *Bridge) Approve(ctx context.Context, t Target, id, decision string, answers map[string]string) error {
	if decision != "accept" && decision != "decline" {
		return errors.New("invalid approval decision")
	}
	var ok bool
	code := nativeApprovalCode(t, id, decision, answers)
	err := b.renderer(ctx, code, &ok)
	if err == nil && !ok {
		return errors.New("Claude Desktop approval not acknowledged")
	}
	return err
}

func nativeApprovalCode(t Target, id, decision string, answers map[string]string) string {
	return `(async()=>{` + sessionCheck(t) + `const p=(s.pendingToolPermissions||[]).find(p=>p.requestId===` + js(id) + `&&p.sessionId===s.sessionId);if(!p||typeof l.respondToToolPermission!=='function')throw Error('expired permission');let input;if(p.toolName==='AskUserQuestion'&&` + js(decision) + `==='accept'){input={...p.input,answers:` + js(answers) + `}}await l.respondToToolPermission(p.requestId,` + js(map[string]string{"accept": "once", "decline": "deny"}[decision]) + `,input);return true})()`
}
