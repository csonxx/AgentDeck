package claudedesktop

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Run the actual renderer expression with a Desktop handler whose promise
// deliberately never resolves. Input acceptance is checked separately by the
// adapter; waiting for this promise would lose the inspector connection.
func TestNativeDispatchDoesNotWaitForTurnCompletion(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("JavaScript contract test requires Node.js")
	}
	target := Target{Host: "local_fixture", Native: "native_fixture", CWD: "/fixture", Project: "/fixture"}
	code := `let calls=[];global.window={'claude.web':{LocalSessions:{
getSession:async()=>({sessionId:'local_fixture',cwd:'/fixture',originCwd:'/fixture'}),
sendMessage:(...args)=>{calls.push(args);return new Promise(()=>{});}
}}};
Promise.race([` + nativeSendCode(target, "owner fixture input", "native-user-uuid") + `,
new Promise(resolve=>setTimeout(()=>resolve('timeout'),500))]).then(result=>{
const a=calls[0];if(result!==true||calls.length!==1||a.length!==8||a[0]!=='local_fixture'||a[1]!=='owner fixture input'||a[7]!=='native-user-uuid'||a.slice(2,7).some(v=>v!==undefined))process.exit(1);
process.exit(0);
}).catch(()=>process.exit(2));`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(code)
	if err := cmd.Run(); err != nil {
		t.Fatal("owner handler waited for completion or changed dispatch identity", err)
	}
}

func runRendererContract(t *testing.T, code string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("JavaScript contract test requires Node.js")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node)
	cmd.Stdin = strings.NewReader(code)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("renderer contract failed: %v %s", err, out)
	}
}

func TestNativeImageDispatchKeepsBinaryAndHumanIdentity(t *testing.T) {
	target := Target{Host: "local_fixture", Native: "native_fixture", CWD: "/fixture", Project: "/fixture"}
	images := []Image{{Base64: "iVBORw0KGgo=", MIME: "image/png", Name: "quote'\".png"}}
	runRendererContract(t, `let calls=[];global.window={'claude.web':{LocalSessions:{getSession:async()=>({sessionId:'local_fixture',cwd:'/fixture'}),sendMessage:(...a)=>{calls.push(a);return new Promise(()=>{})}}}};
`+nativeImageSendCode(target, "text'` ${notCode}", "human-id", images)+`.then(r=>{const a=calls[0];if(r!==true||calls.length!==1||a[7]!=='human-id'||a[2][0].base64!=='iVBORw0KGgo='||a[2][0].mimeType!=='image/png'||a.slice(3,7).some(x=>x!==undefined))process.exit(1);process.exit(0)}).catch(()=>process.exit(2));`)
}

func TestNativePermissionUsesCurrentSessionAndOneTimeDecision(t *testing.T) {
	target := Target{Host: "local_fixture", CWD: "/fixture", Project: "/fixture"}
	for _, decision := range []string{"accept", "decline"} {
		for _, scenario := range []string{"current", "expired", "other-session", "wrong-project"} {
			t.Run(decision+"/"+scenario, func(t *testing.T) {
				want := map[string]string{"accept": "once", "decline": "deny"}[decision]
				setup := `let calls=[];let s={sessionId:'local_fixture',cwd:'/fixture',pendingToolPermissions:[{requestId:'request-id',sessionId:'local_fixture',toolName:'AskUserQuestion',input:{questions:[{question:'pick'}]}}]};`
				switch scenario {
				case "expired":
					setup += `s.pendingToolPermissions=[];`
				case "other-session":
					setup += `s.pendingToolPermissions[0].sessionId='local_other';`
				case "wrong-project":
					setup += `s.cwd='/elsewhere';`
				}
				setup += `global.window={'claude.web':{LocalSessions:{sendMessage:()=>{},getSession:async()=>s,respondToToolPermission:async(...a)=>calls.push(a)}}};`
				check := `if(calls.length!==1||calls[0][0]!=='request-id'||calls[0][1]!==` + js(want) + `)process.exit(1);`
				if decision == "accept" {
					check += `if(calls[0][2].answers.pick!=="owner's answer"||s.pendingToolPermissions[0].input.answers!==undefined)process.exit(2);`
				}
				if scenario != "current" {
					check = `process.exit(3);`
				}
				caught := `process.exit(4);`
				if scenario != "current" {
					caught = `if(calls.length!==0)process.exit(5);process.exit(0);`
				}
				runRendererContract(t, setup+nativeApprovalCode(target, "request-id", decision, map[string]string{"pick": "owner's answer"})+`.then(()=>{`+check+`process.exit(0)}).catch(()=>{`+caught+`});`)
			})
		}
	}
}
