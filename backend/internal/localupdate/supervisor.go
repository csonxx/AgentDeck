package localupdate

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"agentdeck/internal/localipc"
)

type Status struct {
	Managed      bool   `json:"managed"`
	Version      string `json:"version"`
	Build        int64  `json:"build"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Auto         bool   `json:"auto_update"`
	Phase        string `json:"phase"`
	Available    string `json:"available,omitempty"`
	Downloaded   int64  `json:"downloaded"`
	Bytes        int64  `json:"bytes"`
	Checked      string `json:"checked,omitempty"`
	Problem      string `json:"problem,omitempty"`
	LastRollback string `json:"last_rollback,omitempty"`
}

type Supervisor struct {
	checkedAt time.Time
	Dir       string
	HTTP      *http.Client
	// Test seams also make timeout policy explicit. No update has permission to
	// force-close an agent task merely because its idle check takes too long.
	CheckInterval, PollInterval, StartupTimeout, Soak, IdleRetry, RetryInterval time.Duration
	mu                                                                          sync.Mutex
	cfg                                                                         Config
	state                                                                       State
	status                                                                      Status
	wake                                                                        chan struct{}
	checking                                                                    bool
}

func Open(dir string) (*Supervisor, error) {
	s := &Supervisor{Dir: dir, HTTP: HTTPClient(), CheckInterval: 6 * time.Hour, PollInterval: time.Second, StartupTimeout: 30 * time.Minute, Soak: 30 * time.Second, IdleRetry: 30 * time.Second, RetryInterval: 5 * time.Minute, wake: make(chan struct{}, 1)}
	if err := readJSON(filepath.Join(root(dir), "config.json"), &s.cfg); err != nil {
		return nil, err
	}
	if !GatewayURL(s.cfg.Gateway) || len(s.cfg.Keys) == 0 {
		return nil, errors.New("invalid workstation update config")
	}
	for id, key := range s.cfg.Keys {
		if !keyPattern.MatchString(id) || !validPublicKey(key) {
			return nil, errors.New("invalid publisher keys")
		}
	}
	if err := readJSON(filepath.Join(root(dir), "state.json"), &s.state); err != nil {
		return nil, err
	}
	if s.state.Highest < s.state.Active.Build {
		return nil, errors.New("invalid workstation version state")
	}
	if s.state.Trial {
		if s.state.Previous == nil {
			return nil, errors.New("missing rollback version")
		}
		s.state.Rejected = s.state.Active.Build
		s.state.Active = *s.state.Previous
		s.state.Previous = nil
		s.state.Pending = nil
		s.state.Trial = false
		s.state.Problem = "上次更新未完成启动确认，已回退"
		if err := s.save(); err != nil {
			return nil, err
		}
	}
	if !validInstalled(s.state.Active) {
		return nil, errors.New("invalid installed workstation version")
	}
	if s.state.Pending != nil && s.state.Pending.Verify(s.cfg.Keys) != nil {
		return nil, errors.New("invalid pending workstation release")
	}
	s.status = Status{Managed: true, Version: s.state.Active.Version, Build: s.state.Active.Build, OS: runtime.GOOS, Arch: runtime.GOARCH, Auto: s.cfg.Auto, Phase: "starting", LastRollback: s.state.Problem}
	return s, nil
}
func validInstalled(r Release) bool {
	h, err := hex.DecodeString(r.SHA256)
	return err == nil && len(h) == 32 && r.SHA256 == hex.EncodeToString(h) && r.Bytes > 0 && r.Build > 0 && versionPattern.MatchString(r.Version) && r.OS == runtime.GOOS && r.Arch == runtime.GOARCH && r.Protocol == Protocol && r.StateSchema == Schema
}
func (s *Supervisor) save() error      { return saveJSON(filepath.Join(root(s.Dir), "state.json"), s.state) }
func (s *Supervisor) snapshot() Status { s.mu.Lock(); defer s.mu.Unlock(); return s.status }
func (s *Supervisor) phase(phase, problem string) {
	s.mu.Lock()
	s.status.Phase = phase
	s.status.Problem = problem
	s.mu.Unlock()
}
func (s *Supervisor) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Supervisor) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(s.snapshot()) })
	m.HandleFunc("POST /check", func(w http.ResponseWriter, r *http.Request) { s.notify(); w.WriteHeader(http.StatusAccepted) })
	m.HandleFunc("POST /policy", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Auto bool `json:"auto_update"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		dec.DisallowUnknownFields()
		if dec.Decode(&req) != nil {
			http.Error(w, "invalid policy", 400)
			return
		}
		s.mu.Lock()
		cfg := s.cfg
		cfg.Auto = req.Auto
		err := saveJSON(filepath.Join(root(s.Dir), "config.json"), cfg)
		if err == nil {
			s.cfg = cfg
			s.status.Auto = req.Auto
		}
		s.mu.Unlock()
		if err != nil {
			http.Error(w, "policy not saved", 500)
			return
		}
		s.notify()
		w.WriteHeader(200)
	})
	return m
}

func (s *Supervisor) check(ctx context.Context) {
	s.mu.Lock()
	if s.checking || s.state.Trial {
		s.mu.Unlock()
		return
	}
	s.checking = true
	cfg := s.cfg
	s.checkedAt = time.Now()
	s.status.Checked = s.checkedAt.UTC().Format(time.RFC3339)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.checking = false; s.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	// Follow a re-associated gateway without exposing its enrollment key. Before
	// pairing, the installer gateway remains the update source.
	var link struct {
		URL    string `json:"url"`
		Linked bool   `json:"linked"`
	}
	if s.childRequest(ctx, "GET", "/gateway/status", nil, &link) == nil && link.Linked && GatewayURL(link.URL) {
		cfg.Gateway = link.URL
	}
	request, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/v2/local/releases/latest?os=%s&arch=%s", trimSlash(cfg.Gateway), runtime.GOOS, runtime.GOARCH), nil)
	resp, err := s.HTTP.Do(request)
	if err != nil {
		s.phase("retrying", "更新检查失败，将自动重试")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		s.phase("current", "")
		return
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			s.phase("retrying", "网关尚未支持工作站更新，请升级网关")
		} else {
			s.phase("retrying", fmt.Sprintf("更新服务暂不可用（HTTP %d）", resp.StatusCode))
		}
		return
	}
	var release Release
	dec := json.NewDecoder(io.LimitReader(resp.Body, 20<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&release) != nil || release.Verify(cfg.Keys) != nil || release.OS != runtime.GOOS || release.Arch != runtime.GOARCH {
		s.phase("rejected", "更新签名或平台校验失败，未安装")
		return
	}
	s.mu.Lock()
	s.status.Problem = ""
	s.status.Phase = "current"
	if release.Build <= s.state.Active.Build || release.Build < s.state.Highest || release.Build == s.state.Rejected {
		s.mu.Unlock()
		return
	}
	s.status.Available = release.Version
	s.status.Bytes = release.Bytes
	s.status.Downloaded = 0
	s.status.Phase = "downloading"
	s.status.Problem = ""
	s.mu.Unlock()
	err = download(ctx, s.HTTP, s.Dir, release, func(n int64) { s.mu.Lock(); s.status.Downloaded = n; s.mu.Unlock() })
	if err != nil {
		s.phase("retrying", "更新下载未完成，稍后断点续传")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Pending != nil && s.state.Pending.Build == release.Build && *s.state.Pending != release {
		s.status.Problem = "同一版本的发布内容发生变化，已拒绝"
		s.status.Phase = "rejected"
		return
	}
	s.state.Pending = &release
	if release.Build > s.state.Highest {
		s.state.Highest = release.Build
	}
	if err = s.save(); err != nil {
		s.state.Pending = nil
		s.status.Phase = "retrying"
		s.status.Problem = "无法保存已校验的更新"
		return
	}
	s.status.Phase = "waiting_idle"
	slog.Info("local_update_downloaded", "version", release.Version, "build", release.Build)
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
func (s *Supervisor) childRequest(ctx context.Context, method, path string, value, out any) error {
	var b io.Reader
	if value != nil {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		b = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, b)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c := localipc.Client(s.Dir, "control")
	defer c.CloseIdleConnections()
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("local control HTTP %d", res.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(out)
	}
	return nil
}

type process struct {
	cmd               *exec.Cmd
	done              chan error
	instance          string
	started           time.Time
	healthy           time.Time
	activationPending bool
}

func (s *Supervisor) start() (*process, error) {
	s.mu.Lock()
	r := s.state.Active
	args := append([]string(nil), s.cfg.Args...)
	path := s.cfg.Path
	trial := s.state.Trial
	s.mu.Unlock()
	if err := verifyFile(binary(s.Dir, r), r); err != nil {
		return nil, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	p := &process{done: make(chan error, 1), instance: hex.EncodeToString(b), started: time.Now(), activationPending: trial}
	args = append([]string{"serve", "--data", s.Dir}, args...)
	p.cmd = exec.Command(binary(s.Dir, r), args...)
	p.cmd.Env = append(os.Environ(), "AGENTDECK_RUN_ID="+p.instance, fmt.Sprintf("AGENTDECK_UPDATE_TRIAL=%d", boolInt(trial)))
	if path != "" {
		p.cmd.Env = append(p.cmd.Env, "PATH="+path)
	}
	p.cmd.Stdout = os.Stdout
	p.cmd.Stderr = os.Stderr
	if err := p.cmd.Start(); err != nil {
		return nil, err
	}
	go func() { p.done <- p.cmd.Wait() }()
	s.phase("starting", "")
	return p, nil
}
func (s *Supervisor) rollback() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Previous == nil {
		return errors.New("rollback executable missing")
	}
	failed := s.state.Active
	s.state.Active = *s.state.Previous
	s.state.Previous = nil
	s.state.Trial = false
	s.state.Pending = nil
	s.state.Rejected = failed.Build
	s.state.Problem = "更新后启动验证失败，已自动回退"
	s.status.LastRollback = s.state.Problem
	s.status.Version = s.state.Active.Version
	s.status.Build = s.state.Active.Build
	s.status.Available = ""
	slog.Warn("local_update_rolled_back", "rejected_build", failed.Build, "restored_build", s.state.Active.Build)
	return s.save()
}

func (s *Supervisor) Run(ctx context.Context) error {
	ln, err := localipc.Listen(s.Dir, "updates")
	if err != nil {
		return err
	}
	defer ln.Close()
	api := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = api.Serve(ln) }()
	defer api.Close()
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	var checks sync.WaitGroup
	defer func() { cancel(); checks.Wait() }()
	check := func() { checks.Add(1); go func() { defer checks.Done(); s.check(life) }() }
	p, err := s.start()
	if err != nil {
		return err
	}
	defer func() {
		if p != nil {
			_ = p.cmd.Process.Kill()
			<-p.done
		}
	}()
	poll := time.NewTicker(s.PollInterval)
	defer poll.Stop()
	periodic := time.NewTicker(s.CheckInterval)
	defer periodic.Stop()
	check()
	var lastAttempt time.Time
	for {
		select {
		case <-ctx.Done():
			cancel()
			// Owner shutdown is distinct from unattended update. Ask Local to flush
			// and stop first; force termination is only an OS shutdown fallback.
			stopCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.childRequest(stopCtx, "POST", "/update/stop", nil, nil)
			done()
			select {
			case <-p.done:
				p = nil
			case <-time.After(30 * time.Second):
			}
			return nil
		case <-periodic.C:
			check()
		case <-s.wake:
			check()
		case <-p.done:
			p = nil
			s.mu.Lock()
			trial := s.state.Trial
			s.mu.Unlock()
			if trial {
				if err = s.rollback(); err != nil {
					return err
				}
			}
			s.phase("retrying", "工作站进程已退出，正在恢复")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
			p, err = s.start()
			if err != nil {
				return err
			}
		case <-poll.C:
			s.mu.Lock()
			retry := s.status.Phase == "retrying" && !s.checking && time.Since(s.checkedAt) >= s.RetryInterval
			s.mu.Unlock()
			if retry {
				check()
			}
			var health struct {
				Instance string `json:"instance"`
				Version  string `json:"version"`
				Build    string `json:"build"`
			}
			probe, done := context.WithTimeout(life, 2*time.Second)
			err = s.childRequest(probe, "GET", "/update/health", nil, &health)
			done()
			s.mu.Lock()
			active := s.state.Active
			trial := s.state.Trial
			pending := s.state.Pending
			auto := s.cfg.Auto
			checking := s.checking
			keys := s.cfg.Keys
			s.mu.Unlock()
			healthy := err == nil && health.Instance == p.instance && health.Version == active.Version && health.Build == fmt.Sprint(active.Build)
			if !healthy {
				p.healthy = time.Time{}
				if trial && time.Since(p.started) > s.StartupTimeout {
					_ = p.cmd.Process.Kill()
					<-p.done
					p = nil
					if err = s.rollback(); err != nil {
						return err
					}
					p, err = s.start()
					if err != nil {
						return err
					}
				}
				continue
			}
			if p.healthy.IsZero() {
				p.healthy = time.Now()
			}
			if trial {
				if time.Since(p.healthy) < s.Soak {
					continue
				}
				s.mu.Lock()
				s.state.Trial = false
				s.state.Problem = ""
				s.status.Problem = ""
				s.status.LastRollback = ""
				s.status.Phase = "activating"
				s.status.Available = ""
				err = s.save()
				s.mu.Unlock()
				if err != nil {
					return err
				}
			}
			if p.activationPending {
				resumeCtx, resumeDone := context.WithTimeout(life, 2*time.Second)
				err = s.childRequest(resumeCtx, "POST", "/update/resume", nil, nil)
				resumeDone()
				if err != nil {
					s.phase("activating", "正在恢复工作站消息接收")
					continue
				}
				p.activationPending = false
				s.phase("current", "")
				slog.Info("local_update_verified", "version", active.Version, "build", active.Build)
			}
			if pending == nil {
				s.mu.Lock()
				if s.status.Problem == "" && !checking {
					s.status.Phase = "current"
				}
				s.mu.Unlock()
				continue
			}
			if !auto {
				s.phase("paused", "自动安装已关闭，已下载的版本会保留")
				continue
			}
			if checking || time.Since(lastAttempt) < s.IdleRetry {
				continue
			}
			lastAttempt = time.Now()
			if pending.Verify(keys) != nil || verifyFile(binary(s.Dir, *pending), *pending) != nil {
				s.phase("rejected", "更新文件复核失败，未切换")
				continue
			}
			var ready struct {
				Ready  bool   `json:"ready"`
				Reason string `json:"reason"`
			}
			prepCtx, prepDone := context.WithTimeout(life, 5*time.Second)
			err = s.childRequest(prepCtx, "POST", "/update/prepare", nil, &ready)
			prepDone()
			if err != nil || !ready.Ready {
				s.phase("waiting_idle", "有任务或消息正在处理，空闲后自动安装")
				continue
			}
			s.phase("restarting", "")
			stopCtx, stopDone := context.WithTimeout(life, 5*time.Second)
			err = s.childRequest(stopCtx, "POST", "/update/stop", nil, nil)
			stopDone()
			if err != nil {
				_ = s.childRequest(life, "POST", "/update/resume", nil, nil)
				s.phase("retrying", "无法确认工作站已停止，未切换")
				continue
			}
			// Never launch two writers or force-kill active work during an update.
			select {
			case <-p.done:
				p = nil
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Minute):
				s.phase("waiting_idle", "等待工作站完成关闭，未强制终止任务")
				continue
			}
			s.mu.Lock()
			old := s.state.Active
			s.state.Previous = &old
			s.state.Active = *pending
			s.state.Pending = nil
			s.state.Trial = true
			s.status.Version = pending.Version
			s.status.Build = pending.Build
			err = s.save()
			s.mu.Unlock()
			if err != nil {
				return err
			}
			p, err = s.start()
			if err != nil {
				if err = s.rollback(); err != nil {
					return err
				}
				p, err = s.start()
				if err != nil {
					return err
				}
			}
		}
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
