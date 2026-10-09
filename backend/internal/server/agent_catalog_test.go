package server

import (
 "sync/atomic"
 "testing"
 "agentdeck/internal/agent"
)
type changingCatalog struct { agent.Connector; ready atomic.Bool }
func (c *changingCatalog) Info() agent.AgentInfo {
 detail:="connecting";if c.ready.Load(){detail="connected"}
 return agent.AgentInfo{ID:"opencode",Name:"OpenCode",Configured:true,Create:true,Send:true,Detail:detail}
}
func (*changingCatalog) Close() {}
func TestAgentStatusChangesWithoutSessionOutput(t *testing.T) {
 s,ts:=setupServer(t);c:=&changingCatalog{}
 s.Manager=&agent.Manager{Store:s.Store,Connectors:map[string]agent.Connector{"opencode":c}}
 p:=pairPhone(t,s,ts);p.connect(t,ts,s);p.read(t,"sessions")
 c.ready.Store(true)
 catalog:=p.read(t,"agents")["agents"].([]any)
 found:=false;for _,entry:=range catalog { v:=entry.(map[string]any);if v["id"]=="opencode"&&v["detail"]=="connected"{found=true} }
 if !found{t.Fatal("connection status did not update without chat output")}
}
